package marketapp

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"robot/internal/foundation/lockhub"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- auction_guard.go ----
const defaultDFGameRJSPath = "/dp2/df_game_r.js"

const auctionSearchGuardBegin = "// DP2_AUCTION_SEARCH_HOOK_GUARD_BEGIN"
const auctionSearchGuardEnd = "// DP2_AUCTION_SEARCH_HOOK_GUARD_END"
const auctionRejectedRetryEvery = 10
const auctionRejectedRetryDivisor = 100
const auctionSpecialBudgetDivisor = 10
const ceraRejectedTTL = 30 * time.Minute
const marketServiceRestartCooldown = 10 * time.Minute
const specialAddInfoBase int32 = 210000000
const maxInt32 int32 = 2147483647

const auctionSearchGuardSource = auctionSearchGuardBegin + `
(function () {
    var root = (typeof globalThis !== 'undefined') ? globalThis : this;
    var key = '__dp2_auction_search_hook_guard_v1__';
    if (root[key]) {
        return;
    }
    root[key] = true;

    var blocked = {};
    blocked[ptr('0x084D75BC').toString().toLowerCase()] = true;

    var rawReplace = Interceptor.replace.bind(Interceptor);
    var rawRevert = Interceptor.revert.bind(Interceptor);

    function addrOf(target) {
        try {
            return ptr(target).toString().toLowerCase();
        } catch (e) {
            try {
                return target.toString().toLowerCase();
            } catch (_) {
                return '';
            }
        }
    }

    Interceptor.replace = function (target, replacement) {
        var addr = addrOf(target);
        if (blocked[addr]) {
            try {
                rawRevert(target);
                Interceptor.flush();
            } catch (e) {
            }
            console.log('[dp2 guard] blocked auction search Interceptor.replace at ' + addr);
            return;
        }
        return rawReplace(target, replacement);
    };

    console.log('[dp2 guard] auction search hook guard installed');
})();
` + auctionSearchGuardEnd + `

`

func (a *App) InstallAuctionSearchGuard(req AuctionSearchGuardRequest) (AuctionSearchGuardResult, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = defaultDFGameRJSPath
	}
	result := AuctionSearchGuardResult{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Contains(data, []byte(auctionSearchGuardBegin)) {
		result.Installed = true
		result.Message = "auction search hook guard already installed"
		a.appendLog(LogEvent{Type: "auction_guard", Status: marketLogStatusExists, Message: path})
		return result, nil
	}
	backup := fmt.Sprintf("%s.bak_auction_guard_%s", path, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return result, fmt.Errorf("prepare backup dir: %w", err)
	}
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return result, fmt.Errorf("backup %s: %w", backup, err)
	}
	next := append([]byte(auctionSearchGuardSource), data...)
	if err := os.WriteFile(path, next, 0644); err != nil {
		return result, fmt.Errorf("write %s: %w", path, err)
	}
	result.Backup = backup
	result.Installed = true
	result.Changed = true
	result.Message = "auction search hook guard installed; restart df_game_r to apply"
	a.appendLog(LogEvent{Type: "auction_guard", Status: marketLogStatusInstalled, Message: fmt.Sprintf("%s backup=%s", path, backup)})
	return result, nil
}

// ---- auto.go ----
func (a *App) StartAuto() {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoRun {
		return
	}
	a.stopAuto = make(chan struct{})
	a.autoDone = make(chan struct{})
	a.autoRun = true
	a.autoStop = false
	go a.autoLoop()
}

func (a *App) StopAuto() {
	a.stopAutoWithWait(true)
}

func (a *App) StopAutoAsync() {
	a.stopAutoWithWait(false)
}

func (a *App) RestartAutoAsync() {
	a.autoMu.Lock()
	if !a.autoRun {
		a.autoMu.Unlock()
		a.StartAuto()
		return
	}
	if a.autoStop {
		done := a.autoDone
		a.autoMu.Unlock()
		go func() {
			<-done
			a.StartAuto()
		}()
		return
	}
	stop := a.stopAuto
	done := a.autoDone
	a.autoStop = true
	close(stop)
	a.autoMu.Unlock()
	go func() {
		<-done
		a.StartAuto()
	}()
}

func (a *App) startAutoIfEnabled() {
	a.stateMu.Lock()
	enabled := a.cfg.Auto.Enabled
	a.stateMu.Unlock()
	if enabled {
		a.StartAuto()
	}
}

func (a *App) stopAutoWithWait(wait bool) {
	a.autoMu.Lock()
	if !a.autoRun {
		a.autoMu.Unlock()
		return
	}
	if !a.autoStop {
		close(a.stopAuto)
		a.autoStop = true
	}
	done := a.autoDone
	a.autoMu.Unlock()
	if wait {
		<-done
	}
}

func (a *App) Shutdown() {
	a.StopAuto()
}

func (a *App) markAutoStopped() {
	a.autoMu.Lock()
	a.autoRun = false
	a.autoStop = false
	a.autoMu.Unlock()
}

func (a *App) AutoRunning() bool {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return a.autoRun
}

func (a *App) autoLoop() {
	defer func() {
		a.markAutoStopped()
		close(a.autoDone)
	}()
	a.stateMu.Lock()
	enabled := a.cfg.Auto.Enabled
	initialMS := a.cfg.Auto.InitialDelayMS
	intervalMS := a.cfg.Auto.IntervalMS
	a.stateMu.Unlock()
	if !enabled {
		a.appendLog(LogEvent{Type: "auto", Status: marketLogStatusDisabled})
		return
	}
	initial := time.Duration(initialMS) * time.Millisecond
	if initial > 0 {
		select {
		case <-time.After(initial):
		case <-a.stopAuto:
			return
		}
	}
	a.runAutoOnce()
	interval := time.Duration(intervalMS) * time.Millisecond
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.runAutoOnce()
		case <-a.stopAuto:
			return
		}
	}
}

func (a *App) runAutoOnce() {
	if tables, err := a.repository.EnsureMarketTables(a.marketDBNames(), time.Now()); err != nil {
		a.stateMu.Lock()
		a.dbInit = tables
		a.dbInitErr = err.Error()
		a.stateMu.Unlock()
		a.appendLog(LogEvent{Type: "db_init", Status: marketLogStatusFailed, Message: err.Error()})
	} else {
		a.stateMu.Lock()
		a.dbInit = tables
		a.dbInitErr = ""
		a.stateMu.Unlock()
	}
	markets := a.cfg.Auto.Markets
	if len(markets) == 0 {
		markets = []string{marketNameAuction, marketNameCera}
	}
	if !a.dfGameRRunning() {
		a.appendLog(LogEvent{Type: "auto", Status: marketLogStatusGameDown, Message: "df_game_r is not running; market services skipped"})
		for _, market := range markets {
			a.markMarketPolicyBlocked(market, "df_game_r is not running")
		}
		return
	}
	ready := a.ensureMarketServices(markets)
	for _, market := range markets {
		market = strings.ToLower(strings.TrimSpace(market))
		if market == "" {
			continue
		}
		if !ready[marketServiceName(market)] {
			a.appendLog(LogEvent{Type: "auto", Status: marketLogStatusServiceDown, Market: market, Message: "market service is not ready; job skipped"})
			a.markMarketPolicyBlocked(market, "market service is not ready")
			continue
		}
		policy := a.marketAutoPolicy(market, a.cfg.Auto)
		if a.cfg.Collector.Enabled {
			a.appendLog(LogEvent{Type: "auto_collect", Market: market, Status: marketLogStatusStart})
			job, err := a.CollectOnce(CollectRequest{
				Market:          market,
				Execute:         true,
				MaxActions:      policy.MaxActions,
				MaxConcurrent:   policy.MaxConcurrent,
				ContinueOnError: a.cfg.Auto.ContinueOnError,
			})
			status := job.Status
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			a.appendLog(LogEvent{Type: "auto_collect", JobID: job.ID, Market: market, Status: status, Message: msg})
		}
		a.appendLog(LogEvent{Type: "auto_run", Market: market, Status: marketLogStatusStart})
		job, err := a.RestockOnce(RestockRequest{
			Market:          market,
			Execute:         true,
			MaxActions:      policy.MaxActions,
			MaxConcurrent:   policy.MaxConcurrent,
			ContinueOnError: a.cfg.Auto.ContinueOnError,
		})
		status := job.Status
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		a.appendLog(LogEvent{Type: "auto_run", JobID: job.ID, Market: market, Status: status, Message: msg})
		a.recordMarketPolicyJob(market, job)
	}
}

func (a *App) dfGameRRunning() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	name := filepath.Base(strings.TrimSpace(a.dfGameR))
	if name == "." || name == "/" || name == "" {
		name = "df_game_r"
	}
	out, err := exec.Command("pidof", name).Output()
	if err == nil && len(strings.Fields(string(out))) > 0 {
		return true
	}
	out, err = exec.Command("pgrep", "-f", "(^|/)"+regexp.QuoteMeta(name)+"( |$)").Output()
	return err == nil && len(strings.Fields(string(out))) > 0
}

func (a *App) ensureMarketServices(markets []string) map[string]bool {
	ready := map[string]bool{}
	needed := map[string]bool{}
	for _, market := range markets {
		name := marketServiceName(market)
		if name != "" {
			needed[name] = true
		}
	}
	if len(markets) == 0 {
		for _, service := range marketServiceSpecs() {
			needed[service.name] = true
		}
	}
	for _, service := range marketServiceSpecs() {
		if needed[service.name] {
			ready[service.name] = a.ensureMarketService(service)
		}
	}
	return ready
}

func marketServiceName(market string) string {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case marketNameCera, marketAliasGold, marketAliasPoint:
		return marketServiceNamePoint
	case "", marketNameAuction:
		return marketServiceNameAuction
	default:
		return ""
	}
}

func (a *App) ensureMarketService(service marketServiceSpec) bool {
	status := MarketServiceStatus{Name: service.name, Addr: service.addr, Dir: service.dir, Bin: service.bin, CheckedAt: time.Now(), LogPath: a.marketServiceLogPath(service.name)}
	if tcpReady(service.addr, 500*time.Millisecond) {
		status.Listening = true
		status.PID = marketServicePID(service.bin)
		if status.PID <= 0 {
			status.Status = MarketServiceStatusPortReadyProcessMissing
			status.Message = "port is listening but target process was not found"
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
			return false
		}
		if staleReason := a.marketServiceStaleItemInfoReason(service, status.PID); staleReason != "" {
			status.Status = MarketServiceStatusDown
			status.Message = staleReason
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: marketLogStatusStaleItemInfo, Message: staleReason})
			if err := a.stopMarketServiceForItemInfo(service.name, service.addr, service.bin); err != nil {
				status.Status = MarketServiceStatusStartFailed
				status.Message = err.Error()
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				return false
			}
		} else {
			status.Status = MarketServiceStatusReady
			status.Message = "already listening"
			a.setMarketServiceStatus(status)
			return true
		}
	}
	if err := prepareMarketServiceDir(service.dir); err != nil {
		status.Status = MarketServiceStatusPrepareFailed
		status.Message = err.Error()
		a.setMarketServiceStatus(status)
		a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
		return false
	}
	status.StartedAt = time.Now()
	cmdline := marketServiceShellCommand(service.bin, service.args, status.LogPath)
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Dir = service.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		status.Status = MarketServiceStatusStartFailed
		status.Message = err.Error()
		a.setMarketServiceStatus(status)
		a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
		return false
	}
	a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: marketLogStatusStart, Message: fmt.Sprintf("addr=%s output=%s", service.addr, strings.TrimSpace(string(out)))})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if tcpReady(service.addr, 500*time.Millisecond) {
			status.Listening = true
			status.PID = marketServicePID(service.bin)
			time.Sleep(8 * time.Second)
			status.CheckedAt = time.Now()
			status.Listening = tcpReady(service.addr, 500*time.Millisecond)
			status.PID = marketServicePID(service.bin)
			if hasMarketServiceFailure(status.LogPath) {
				status.Status = MarketServiceStatusRegistItemFailed
				status.Message = "service log contains RegistItem failure"
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				return false
			}
			if status.PID <= 0 {
				status.Status = MarketServiceStatusProcessExited
				status.Message = "process exited during startup stability window"
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				return false
			}
			if !status.Listening {
				status.Status = MarketServiceStatusPortReadyButUnstable
				status.Message = "port stopped listening during startup stability window"
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				return false
			}
			status.Status = MarketServiceStatusReady
			status.Message = service.addr
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	status.Status = MarketServiceStatusStartTimeout
	status.Message = service.addr
	a.setMarketServiceStatus(status)
	a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
	return false
}

func (a *App) refreshMarketServiceStatuses() {
	for _, service := range marketServiceSpecs() {
		a.refreshMarketServiceStatus(service)
	}
}

func (a *App) refreshMarketServiceStatus(service marketServiceSpec) {
	status := MarketServiceStatus{
		Name:      service.name,
		Addr:      service.addr,
		Dir:       service.dir,
		Bin:       service.bin,
		CheckedAt: time.Now(),
		LogPath:   a.marketServiceLogPath(service.name),
		PID:       marketServicePID(service.bin),
		Listening: tcpReady(service.addr, 300*time.Millisecond),
	}
	switch {
	case status.Listening && status.PID > 0:
		status.Status = MarketServiceStatusReady
		status.Message = "already listening"
	case status.Listening:
		status.Status = MarketServiceStatusPortReadyProcessMissing
		status.Message = "port is listening but target process was not found"
	case status.PID > 0:
		status.Status = MarketServiceStatusProcessWithoutPort
		status.Message = "target process exists but port is not listening"
	default:
		status.Status = MarketServiceStatusDown
		status.Message = "not running"
	}
	a.setMarketServiceStatus(status)
}

func (a *App) marketServiceStaleItemInfoReason(service marketServiceSpec, pid int) string {
	if runtime.GOOS != "linux" || pid <= 0 {
		return ""
	}
	path := a.itemInfoTargetForService(service.name)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	started, err := linuxProcessStartTime(pid)
	if err != nil {
		return ""
	}
	if !info.ModTime().After(started.Add(time.Second)) {
		return ""
	}
	return fmt.Sprintf("%s is newer than %s start: iteminfo=%s service=%s", filepath.Base(path), service.name, info.ModTime().Format(time.RFC3339), started.Format(time.RFC3339))
}

func (a *App) itemInfoTargetForService(serviceName string) string {
	needle := "/" + serviceName + "/"
	for _, target := range a.cfg.ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target != "" && strings.Contains(filepath.ToSlash(target), needle) {
			return target
		}
	}
	return ""
}

type marketServiceSpec struct {
	name string
	addr string
	dir  string
	bin  string
	args []string
}

func marketServiceSpecs() []marketServiceSpec {
	return []marketServiceSpec{
		{name: marketServiceNameAuction, addr: "127.0.0.1:30803", dir: "/home/neople/auction", bin: "./df_auction_r", args: []string{"./cfg/auction_cain.cfg", "start", "./df_auction_r"}},
		{name: marketServiceNamePoint, addr: "127.0.0.1:30603", dir: "/home/neople/point", bin: "./df_point_r", args: []string{"./cfg/point_cain.cfg", "start", "df_point_r"}},
	}
}

func marketServiceSpecByName(name string) (marketServiceSpec, bool) {
	for _, service := range marketServiceSpecs() {
		if service.name == name {
			return service, true
		}
	}
	return marketServiceSpec{}, false
}

func (a *App) restartMarketServicesAfterItemInfo() error {
	if runtime.GOOS != "linux" {
		a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusSkipped, Message: "market service restart is linux only"})
		return nil
	}
	for _, service := range marketServiceSpecs() {
		if err := a.stopMarketServiceForItemInfo(service.name, service.addr, service.bin); err != nil {
			return err
		}
		if !a.ensureMarketService(service) {
			return fmt.Errorf("%s restart failed: service is not ready", service.name)
		}
	}
	a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusSuccess, Message: "auction and point services restarted"})
	return nil
}

func (a *App) stopMarketServiceForItemInfo(name, addr, bin string) error {
	process := filepath.Base(strings.TrimSpace(bin))
	if process == "" || process == "." || process == "/" {
		return fmt.Errorf("%s stop failed: invalid process name %q", name, bin)
	}
	pid := marketServicePID(bin)
	if pid <= 0 && !tcpReady(addr, 200*time.Millisecond) {
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopSkipped, Message: "process and port are already down"})
		return nil
	}
	_ = exec.Command("pkill", "-TERM", "-x", process).Run()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopped, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-x", process).Run()
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusKilled, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s stop timeout: %s still running or port still listening", name, process)
}

func (a *App) marketServiceLogPath(name string) string {
	if a.configDir == "" {
		return filepath.Join(os.TempDir(), "robot_market_"+name+".log")
	}
	return filepath.Join(a.configDir, "market_"+name+"_service.log")
}

func (a *App) setMarketServiceStatus(status MarketServiceStatus) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.services == nil {
		a.services = map[string]MarketServiceStatus{}
	}
	a.services[status.Name] = status
}

func marketServicePID(bin string) int {
	name := filepath.Base(strings.TrimSpace(bin))
	if name == "" || name == "." || name == "/" {
		return 0
	}
	out, err := exec.Command("pidof", name).Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return pid
}

func hasMarketServiceFailure(logPath string) bool {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "fail to registitem") ||
		strings.Contains(text, "process exits") ||
		strings.Contains(text, "fatal")
}

func prepareMarketServiceDir(dir string) error {
	if err := os.Chmod(dir, 0777); err != nil && !os.IsPermission(err) {
		return err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "pid", "*.pid"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func marketServiceShellCommand(bin string, args []string, outputPath string) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, shellQuote(bin))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	if outputPath == "" {
		outputPath = "/dev/null"
	}
	return "nohup " + strings.Join(parts, " ") + " >" + shellQuote(outputPath) + " 2>&1 &"
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}

func tcpReady(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ---- collector.go ----
type collectRow struct {
	Market       string
	AuctionID    uint64
	OwnerID      uint32
	ItemID       uint32
	Count        int32
	StartPrice   int32
	InstantPrice int32
}

func (a *App) CollectPlan(req CollectRequest) (PlanResult, error) {
	result := PlanResult{GeneratedAt: time.Now()}
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" || market == marketNameAuction {
		rows, err := a.repository.LoadCollectRows(a.cfg.AuctionDB, marketNameAuction, a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	if market == "" || market == marketNameCera || market == marketAliasGold {
		rows, err := a.repository.LoadCollectRows(a.cfg.CeraDB, marketNameCera, a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	result.Summary.Actions = len(result.Actions)
	for _, action := range result.Actions {
		switch action.Market {
		case marketNameAuction:
			result.Summary.AuctionActions++
		case marketNameCera:
			result.Summary.CeraActions++
		}
	}
	if req.MaxActions > 0 && len(result.Actions) > req.MaxActions {
		result.Actions = result.Actions[:req.MaxActions]
	}
	a.appendLog(LogEvent{Type: "collect_plan", Market: market, Summary: &result.Summary})
	return result, nil
}

func (r SQLRepository) LoadCollectRows(dbName, market string, systemOwnerBase uint32, includeSystemOwners bool) ([]collectRow, error) {
	ownerClause := "owner_id < ?"
	if includeSystemOwners {
		ownerClause = "owner_id >= 0 AND ? >= 0"
	}
	return r.loadCollectRowsWhere(dbName, market, ownerClause, systemOwnerBase)
}

func (r SQLRepository) LoadSystemCollectRows(dbName, market string, systemOwnerBase uint32) ([]collectRow, error) {
	return r.loadCollectRowsWhere(dbName, market, "owner_id >= ?", systemOwnerBase)
}

func (r SQLRepository) loadCollectRowsWhere(dbName, market, ownerClause string, systemOwnerBase uint32) ([]collectRow, error) {
	extraClause := ""
	if market == marketNameCera {
		extraClause = " AND price = -1 AND instant_price > 0"
	}
	query := fmt.Sprintf(
		"SELECT auction_id,owner_id,item_id,IFNULL(add_info,0),IFNULL(price,0),IFNULL(instant_price,0) FROM %s.`auction_main` WHERE %s%s ORDER BY auction_id ASC",
		quoteIdent(dbName), ownerClause, extraClause,
	)
	rows, err := r.db.Query(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []collectRow
	for rows.Next() {
		var row collectRow
		var count, start, instant sql.NullInt64
		row.Market = market
		if err := rows.Scan(&row.AuctionID, &row.OwnerID, &row.ItemID, &count, &start, &instant); err != nil {
			return nil, err
		}
		if count.Valid {
			row.Count = int32(count.Int64)
		}
		if start.Valid {
			row.StartPrice = int32(start.Int64)
		}
		if instant.Valid {
			row.InstantPrice = int32(instant.Int64)
		}
		if row.AuctionID == 0 {
			continue
		}
		if row.InstantPrice <= 0 {
			row.InstantPrice = row.StartPrice
		}
		if row.InstantPrice <= 0 {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r SQLRepository) CountSystemStock(dbName string, systemOwnerBase uint32) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.`auction_main` WHERE owner_id >= ?", quoteIdent(dbName))
	var count int
	if err := r.db.QueryRow(query, systemOwnerBase).Scan(&count); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (r SQLRepository) DeleteSystemStock(dbName string, systemOwnerBase uint32) (int64, error) {
	query := fmt.Sprintf("DELETE FROM %s.`auction_main` WHERE owner_id >= ?", quoteIdent(dbName))
	res, err := r.db.Exec(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return res.RowsAffected()
}

func (a *App) appendCollectActions(rows []collectRow, result *PlanResult) {
	for i, row := range rows {
		buyerID := a.cfg.SystemOwner.BuyerBase + uint32(i%maxInt(a.cfg.SystemOwner.RotateEvery, 1))
		result.Actions = append(result.Actions, Action{
			Market:       row.Market,
			Kind:         "collect",
			Operation:    "collect",
			ItemID:       row.ItemID,
			Count:        row.Count,
			UnitPrice:    row.InstantPrice,
			TotalPrice:   row.InstantPrice,
			OwnerID:      buyerID,
			OwnerName:    a.cfg.SystemOwner.OwnerName,
			CountAddInfo: row.Count,
			StartPrice:   row.StartPrice,
			InstantPrice: row.InstantPrice,
			AuctionID:    row.AuctionID,
			Source:       "auction_main",
		})
	}
}

func (a *App) CollectOnce(req CollectRequest) (JobSummary, error) {
	if !a.jobMu.TryLock() {
		job := busyMarketJob("collect")
		return job, fmt.Errorf(job.Error)
	}
	defer a.jobMu.Unlock()
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("collect-%d", start.UnixNano()),
		Kind:      "collect",
		Status:    MarketJobStatusRunning,
		StartedAt: start,
	}
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.CollectPlan(req)
	if err != nil {
		job.Status = MarketJobStatusFailed
		job.Error = err.Error()
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error})
		return job, err
	}
	job.Plan = &plan.Summary
	maxActions := req.MaxActions
	if maxActions <= 0 {
		maxActions = a.cfg.Collector.MaxActions
	}
	actions := plan.Actions
	if maxActions > 0 && len(actions) > maxActions {
		actions = actions[:maxActions]
	}
	if !req.Execute {
		job.Status = MarketJobStatusPlanned
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
		return job, nil
	}
	failedActions, entries, firstErr := a.executeActions(job.ID, actions, req.MaxConcurrent, req.ContinueOnError, &job)
	a.reconcileCeraLanding(entries)
	if firstErr != nil && !req.ContinueOnError {
		job.Status = MarketJobStatusPartialFailed
		job.Error = firstErr.Error()
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error, Summary: job.Plan})
		return job, firstErr
	}
	if failedActions > 0 {
		job.Status = MarketJobStatusPartialFailed
		job.Error = fmt.Sprintf("%d actions failed", failedActions)
	} else {
		job.Status = MarketJobStatusSuccess
	}
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan, Message: job.Error})
	return job, firstErr
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- db.go ----
var mysqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (a *App) marketDBNames() []string {
	return []string{a.cfg.AuctionDB, a.cfg.CeraDB}
}

func (r SQLRepository) EnsureMarketTables(dbNames []string, now time.Time) ([]string, error) {
	seen := map[string]bool{}
	var ensured []string
	for _, dbName := range dbNames {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" || seen[dbName] {
			continue
		}
		seen[dbName] = true
		tables, err := r.ensureAuctionMonthlyTables(dbName, now)
		ensured = append(ensured, tables...)
		if err != nil {
			return ensured, err
		}
	}
	return ensured, nil
}

func (r SQLRepository) ensureAuctionMonthlyTables(dbName string, now time.Time) ([]string, error) {
	if !mysqlIdentifierPattern.MatchString(dbName) {
		return nil, fmt.Errorf("invalid auction db %q", dbName)
	}
	yyyymm := now.Format("200601")
	targets := []struct {
		base string
		name string
	}{
		{base: "auction_history", name: "auction_history_" + yyyymm},
		{base: "auction_history_buyer", name: "auction_history_buyer_" + yyyymm},
	}
	created := make([]string, 0, len(targets))
	for _, target := range targets {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` LIKE `%s`.`%s`", dbName, target.name, dbName, target.base)
		if _, err := r.db.Exec(query); err != nil {
			return created, fmt.Errorf("ensure monthly table %s.%s: %w", dbName, target.name, err)
		}
		created = append(created, dbName+"."+target.name)
	}
	return created, nil
}

func (a *App) loadSystemStock() (map[uint32]int, map[uint32]int, map[uint32]int, error) {
	occ := map[uint32]int{}
	auctionHave, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	ceraHave, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	return occ, auctionHave, ceraHave, nil
}

func (r SQLRepository) LoadMarketStock(dbName string, systemOwnerBase uint32, occ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	query := "SELECT owner_id,item_id,COUNT(*) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE owner_id >= ? GROUP BY owner_id,item_id"
	rows, err := r.db.Query(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return out, nil
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID, itemID uint32
		var count int
		if err := rows.Scan(&ownerID, &itemID, &count); err != nil {
			return nil, err
		}
		occ[ownerID] += count
		out[itemID] += count
	}
	return out, rows.Err()
}

func (r SQLRepository) LoadMaxAddInfo(dbName string, min int32) (int32, error) {
	query := "SELECT IFNULL(MAX(add_info),0) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE add_info >= ?"
	var max sql.NullInt64
	if err := r.db.QueryRow(query, min).Scan(&max); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	if !max.Valid || max.Int64 <= 0 {
		return 0, nil
	}
	if max.Int64 > int64(maxInt32) {
		return maxInt32, nil
	}
	return int32(max.Int64), nil
}

func (r SQLRepository) CreateCreatureItem(dbName string, ownerID uint32, itemID uint32) (int32, error) {
	table := quoteIdent(dbName) + ".`creature_items`"
	queries := []string{
		"INSERT INTO " + table + " " +
			"(`charac_no`,`slot`,`it_id`,`reg_date`,`name`,`stomach`,`exp`,`endurance`,`creature_type`,`creature_level`,`item_lock`,`delete_flag`,`skills`,`expire_time`,`item_creature_expire_time`) " +
			"VALUES (?,0,?,NOW(),'',100,0,0,0,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')",
		"INSERT INTO " + table + " " +
			"(`charac_no`,`slot`,`it_id`,`reg_date`,`name`,`stomach`,`exp`,`endurance`,`creature_type`,`no_charge`,`stat`,`item_lock_key`,`ipg_agency_no`,`expire_date`,`delete_date`) " +
			"VALUES (?,0,?,NOW(),'',100,0,0,0,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')",
	}
	var lastErr error
	for _, query := range queries {
		id, err := r.insertCreatureItem(query, ownerID, itemID)
		if err == nil {
			return id, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

func (r SQLRepository) insertCreatureItem(query string, ownerID uint32, itemID uint32) (int32, error) {
	result, err := r.db.Exec(query, ownerID, itemID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if id <= 0 || id > int64(maxInt32) {
		return 0, fmt.Errorf("creature ui_id out of range: %d", id)
	}
	return int32(id), nil
}

func (r SQLRepository) CountSystemCreatureItems(dbName string, systemOwnerBase uint32) (int, error) {
	query := "SELECT COUNT(*) FROM " + quoteIdent(dbName) + ".`creature_items` WHERE `charac_no` >= ?"
	var count int
	if err := r.db.QueryRow(query, systemOwnerBase).Scan(&count); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (r SQLRepository) DeleteSystemCreatureItems(dbName string, systemOwnerBase uint32) (int64, error) {
	query := "DELETE FROM " + quoteIdent(dbName) + ".`creature_items` WHERE `charac_no` >= ?"
	result, err := r.db.Exec(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return result.RowsAffected()
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "unknown database") || strings.Contains(msg, "no such table")
}

func quoteIdent(v string) string {
	parts := strings.Split(v, ".")
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}

// ---- auction_plan.go ----
func (a *App) planAuction(rows []restockRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.Quantity <= 0 || !row.Enabled {
			continue
		}
		if row.Kind == "" {
			if catalogItem, ok := catalog[row.ItemID]; ok {
				row.applyMarketItem(catalogItem)
			}
		}
		item := row.marketItem()
		if item.Name == "" {
			item.Name = row.Name
		}
		if item.Kind == "blocked" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: "not_auctionable"})
			continue
		}
		if isAvatarEquipment(item) {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: "avatar_not_auctionable"})
			continue
		}
		if special := specialAuctionKind(item); special != "" {
			a.planSpecialAuction(row, item, special, have, occ, result)
			continue
		}
		if isRiskyPVFItem(item) {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: "risky_special_type"})
			continue
		}
		isEquip := item.Kind == "equipment"
		if row.SealFlag != 0 && !isEquip {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: row.Name, Reason: "requires_add_info"})
			continue
		}
		stackSize := row.StackSize
		if stackSize <= 0 {
			stackSize = 1
		}
		if isEquip {
			stackSize = 1
		}
		if !isEquip && item.StackLimit > 0 && stackSize > item.StackLimit {
			stackSize = item.StackLimit
		}
		targetRecords := (row.Quantity + stackSize - 1) / stackSize
		current := have[row.ItemID]
		if current > 0 {
			continue
		}
		batchInflate := 1.0
		if isEquip {
			batchInflate = float64(randRange(a.rand, a.cfg.Restock.EquipInflateMin, a.cfg.Restock.EquipInflateMax))
		}
		for i := 0; i < targetRecords; i++ {
			pos := i
			count := int32(1)
			if !isEquip {
				if pos < targetRecords-1 {
					count = int32(stackSize)
				} else {
					count = int32(row.Quantity - (targetRecords-1)*stackSize)
				}
			}
			addInfo := int32(0)
			upgrade := 0
			extraAddInfo := int32(0)
			if isEquip {
				addInfo = 0
				upgrade = row.Upgrade
				if upgrade <= 0 {
					upgrade = randRange(a.rand, a.cfg.Restock.UpgradeMin, a.cfg.Restock.UpgradeMax)
				}
			} else {
				addInfo = count
			}
			unit := a.auctionUnitPrice(row.SystemPrice, isEquip, batchInflate, upgrade)
			total := unit
			if !isEquip {
				total = unit * count
			}
			ownerID := a.pickOwner(occ)
			source := row.Source
			if source == "" {
				source = marketActionSourceUnknown
			}
			result.Actions = append(result.Actions, Action{
				Market:       marketNameAuction,
				Kind:         item.Kind,
				ItemID:       row.ItemID,
				ItemType:     item.ItemType,
				Name:         item.Name,
				Count:        count,
				UnitPrice:    unit,
				TotalPrice:   total,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.OwnerName,
				CountAddInfo: addInfo,
				StartPrice:   total - 1,
				InstantPrice: total,
				Upgrade:      upgrade,
				ExtraAddInfo: extraAddInfo,
				Source:       source,
			})
		}
		have[row.ItemID] = targetRecords
	}
}

func (a *App) planSpecialAuction(row restockRow, item catalogItem, special string, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	if have[row.ItemID] > 0 {
		return
	}
	records := row.Quantity
	if records <= 0 {
		records = randRange(a.rand, a.cfg.Restock.EquipmentQtyMin, a.cfg.Restock.EquipmentQtyMax)
	}
	if records <= 0 {
		records = 1
	}
	batchInflate := float64(randRange(a.rand, a.cfg.Restock.EquipInflateMin, a.cfg.Restock.EquipInflateMax))
	planned := 0
	for i := 0; i < records; i++ {
		unit := a.auctionUnitPrice(row.SystemPrice, true, batchInflate, 0)
		ownerID := a.pickOwner(occ)
		action := Action{
			Market:       marketNameAuction,
			Kind:         special,
			ItemID:       row.ItemID,
			ItemType:     item.ItemType,
			Name:         item.Name,
			Count:        1,
			UnitPrice:    unit,
			TotalPrice:   unit,
			OwnerID:      ownerID,
			OwnerName:    a.cfg.SystemOwner.OwnerName,
			StartPrice:   unit - 1,
			InstantPrice: unit,
			Source:       row.Source,
		}
		if special == "creature" {
			uiID, err := a.repository.CreateCreatureItem(a.cfg.GameDB, ownerID, row.ItemID)
			if err != nil {
				result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: "creature_instance_failed"})
				continue
			}
			action.CountAddInfo = uiID
		} else {
			action.CountAddInfo = a.nextSpecialAddInfo()
		}
		result.Actions = append(result.Actions, action)
		planned++
	}
	if planned > 0 {
		have[row.ItemID] = planned
	}
}

// ---- cera_plan.go ----
func (a *App) planCera(rows []ceraRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	type pendingCera struct {
		row  ceraRow
		need int
	}
	pending := make([]pendingCera, 0, len(rows))
	for _, row := range rows {
		if row.ItemID == 0 || row.RestockQty <= 0 || !row.Enabled {
			continue
		}
		if reason := a.ceraRejectedReason(row.ItemID); reason != "" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameCera, ItemID: row.ItemID, Name: row.Label, Reason: reason})
			continue
		}
		if catalog != nil {
			if _, ok := catalog[row.ItemID]; !ok {
				result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameCera, ItemID: row.ItemID, Name: row.Label, Reason: "missing_from_pvf"})
				continue
			}
		}
		current := have[row.ItemID]
		need := row.RestockQty - current
		if need > 0 {
			pending = append(pending, pendingCera{row: row, need: need})
		}
	}
	for {
		added := false
		for i := range pending {
			if pending[i].need <= 0 {
				continue
			}
			row := pending[i].row
			ownerID := a.pickOwner(occ)
			price := a.price(row.RestockPrice)
			result.Actions = append(result.Actions, Action{
				Market:       marketNameCera,
				Kind:         marketAliasGold,
				ItemID:       row.ItemID,
				Name:         row.Label,
				Count:        1,
				UnitPrice:    price,
				TotalPrice:   price,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.CeraName,
				CountAddInfo: 1,
				StartPrice:   -1,
				InstantPrice: price,
				Source:       marketActionSourceCeraConfig,
			})
			pending[i].need--
			added = true
		}
		if !added {
			return
		}
	}
}

// ---- cera_recovery.go ----
func (a *App) ceraRejectedReason(itemID uint32) string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.ceraRejected == nil {
		return ""
	}
	reason := a.ceraRejected[itemID]
	if reason == "" {
		return ""
	}
	if t, ok := a.ceraRejectedAt[itemID]; ok && time.Since(t) > ceraRejectedTTL {
		delete(a.ceraRejected, itemID)
		delete(a.ceraRejectedAt, itemID)
		return ""
	}
	return reason
}

func (a *App) markCeraRejected(itemID uint32, reason string) {
	if itemID == 0 {
		return
	}
	if reason == "" {
		reason = "cera_unlanded"
	}
	a.stateMu.Lock()
	if a.ceraRejected == nil {
		a.ceraRejected = map[uint32]string{}
	}
	if a.ceraRejectedAt == nil {
		a.ceraRejectedAt = map[uint32]time.Time{}
	}
	if _, exists := a.ceraRejected[itemID]; !exists {
		a.ceraRejected[itemID] = reason
	}
	a.ceraRejectedAt[itemID] = time.Now()
	a.stateMu.Unlock()
	a.appendLog(LogEvent{Type: "cera_rejected", Market: marketNameCera, Status: marketLogStatusActive, Message: fmt.Sprintf("item_id=%d reason=%s", itemID, reason)})
}

func (a *App) ceraRejectedCount() int {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if len(a.ceraRejected) == 0 {
		return 0
	}
	now := time.Now()
	count := 0
	for itemID, reason := range a.ceraRejected {
		if reason == "" {
			continue
		}
		if t, ok := a.ceraRejectedAt[itemID]; ok && now.Sub(t) > ceraRejectedTTL {
			delete(a.ceraRejected, itemID)
			delete(a.ceraRejectedAt, itemID)
			continue
		}
		count++
	}
	return count
}

func (a *App) resetCeraRejected() {
	a.stateMu.Lock()
	a.ceraRejected = nil
	a.ceraRejectedAt = nil
	a.stateMu.Unlock()
}

func (a *App) reconcileCeraLanding(entries []ActionEntry) {
	if a.repository == nil {
		return
	}
	okIDs := map[uint32]bool{}
	for _, entry := range entries {
		if entry.Action.Market == marketNameCera && entry.Action.Operation == "" && entry.OK && entry.Action.ItemID != 0 {
			okIDs[entry.Action.ItemID] = true
		}
	}
	if len(okIDs) == 0 {
		return
	}
	time.Sleep(3 * time.Second)
	have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		a.appendLog(LogEvent{Type: "cera_landing", Market: marketNameCera, Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	missing := 0
	for itemID := range okIDs {
		if have[itemID] <= 0 {
			missing++
			a.markCeraRejected(itemID, "cera_unlanded")
		}
	}
	if missing == 0 {
		return
	}
	a.appendLog(LogEvent{Type: "cera_landing", Market: marketNameCera, Status: marketLogStatusFailed, Message: fmt.Sprintf("ok_items=%d missing=%d db_kinds=%d", len(okIDs), missing, len(have))})
	if len(have) == 0 {
		a.restartMarketService(marketServiceNamePoint, "cera ok packets did not land in database")
	}
}

func (a *App) reconcileCeraRejects(entries []ActionEntry) {
	if a.repository == nil {
		return
	}
	total := 0
	rejected118 := 0
	rejectedItems := map[uint32]bool{}
	for _, entry := range entries {
		if entry.Action.Market != marketNameCera || entry.Action.Operation != "" {
			continue
		}
		total++
		if !entry.OK && entry.Reason != nil && *entry.Reason == 118 {
			rejected118++
			if entry.Action.ItemID != 0 {
				rejectedItems[entry.Action.ItemID] = true
			}
		}
	}
	for itemID := range rejectedItems {
		a.markCeraRejected(itemID, "cera_rejected_118")
	}
	if total == 0 || rejected118 != total {
		return
	}
	have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		a.appendLog(LogEvent{Type: "cera_reject", Market: marketNameCera, Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	if len(have) != 0 {
		return
	}
	a.appendLog(LogEvent{Type: "cera_reject", Market: marketNameCera, Status: marketLogStatusFailed, Message: "all cera actions rejected reason=118 while cera db is empty"})
	a.restartMarketService(marketServiceNamePoint, "cera reason 118 with empty database")
}

// ---- recovery_guard.go ----
func (a *App) restartMarketService(name, reason string) {
	if !a.allowMarketServiceRestart(name, reason) {
		return
	}
	if a.restarter != nil {
		a.restarter(name, reason)
		return
	}
	service, ok := marketServiceSpecByName(name)
	if !ok {
		return
	}
	a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusRestart, Message: reason})
	if err := a.stopMarketServiceForItemInfo(service.name, service.addr, service.bin); err != nil {
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	if !a.ensureMarketService(service) {
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusFailed, Message: "restart service is not ready"})
	}
}

func (a *App) allowMarketServiceRestart(name, reason string) bool {
	now := time.Now()
	a.stateMu.Lock()
	if a.lastServiceRestart == nil {
		a.lastServiceRestart = map[string]time.Time{}
	}
	last := a.lastServiceRestart[name]
	if !last.IsZero() && now.Sub(last) < marketServiceRestartCooldown {
		a.stateMu.Unlock()
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusSkipped, Message: fmt.Sprintf("restart cooldown active reason=%s remaining=%s", reason, marketServiceRestartCooldown-now.Sub(last))})
		return false
	}
	a.lastServiceRestart[name] = now
	a.stateMu.Unlock()
	return true
}

// ---- pricing.go ----
func (a *App) price(base int32) int32 {
	if base <= 0 {
		base = 1
	}
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low <= 0 || high <= 0 || low == high {
		return base
	}
	v := float64(base) * (low + a.rand.Float64()*(high-low))
	if v < 1 {
		return 1
	}
	return int32(v)
}

func (a *App) auctionUnitPrice(base int32, isEquipment bool, batchInflate float64, upgrade int) int32 {
	if !isEquipment {
		return a.price(base)
	}
	if base <= 0 {
		base = 1000
	}
	if batchInflate <= 0 {
		batchInflate = 1
	}
	price := float64(base) * batchInflate
	price *= 1 + float64(upgrade)*a.cfg.Restock.UpgradePriceRate
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low > 0 && high > 0 && low != high {
		if high < low {
			high = low
		}
		price *= low + a.rand.Float64()*(high-low)
	}
	if price < 1 {
		return 1
	}
	const maxAuctionPrice = int32(2_000_000_000)
	if price > float64(maxAuctionPrice) {
		return maxAuctionPrice
	}
	return int32(price)
}

func marketBasePrice(item catalogItem) int32 {
	base := item.Price
	if base <= 0 {
		base = item.Value
	}
	if base <= 0 {
		base = 1000
	}
	return base
}

func (a *App) pickOwner(occ map[uint32]int) uint32 {
	owner := a.cfg.SystemOwner.IDBase
	for occ[owner] >= a.cfg.SystemOwner.RotateEvery {
		owner++
	}
	occ[owner]++
	return owner
}

func (a *App) nextSpecialAddInfo() int32 {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.specialAddInfo < specialAddInfoBase {
		a.specialAddInfo = specialAddInfoBase
		if a.repository != nil {
			if max, err := a.repository.LoadMaxAddInfo(a.cfg.AuctionDB, specialAddInfoBase); err == nil && max >= a.specialAddInfo && max < maxInt32 {
				a.specialAddInfo = max + 1
			}
		}
	}
	if a.specialAddInfo <= 0 || a.specialAddInfo >= maxInt32 {
		a.specialAddInfo = specialAddInfoBase
	}
	v := a.specialAddInfo
	a.specialAddInfo++
	return v
}

// ---- source.go ----
//
//go:embed seeds/market_fallback_seed.json
var seedFiles embed.FS

type fallbackSeed struct {
	Core []corePoolItem `json:"core"`
}

func cleanupLegacyMarketFiles(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "market_pool.json"))
	_ = os.Remove(filepath.Join(configDir, "market_restock.json"))
	_ = os.Remove(filepath.Join(configDir, "market_probe_pool.json"))
}

func (a *App) nextAuctionQueueSelection(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int) (auctionQueueSelection, error) {
	if a.auctionQueueNeedsReload(pvfReady) {
		if err := a.reloadAuctionQueues(pvfReady, catalog); err != nil {
			return auctionQueueSelection{}, err
		}
	}

	a.stateMu.Lock()
	a.reconcileRejectedStockLocked(have, pvfReady, catalog)
	budget := a.auctionQueueBudgetLocked(maxActions)
	rows := a.selectAuctionQueueRowsLocked(pvfReady, catalog, have, maxActions, budget)
	a.stateMu.Unlock()
	return auctionQueueSelection{Rows: rows, Budget: budget}, nil
}

func (a *App) auctionQueueNeedsReload(pvfReady bool) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return len(a.auctionQueue) == 0 && len(a.auctionSpecialQueue) == 0 || pvfReady && a.auctionQueueSource != marketQueueSourcePVFItemInfo
}

func (a *App) reloadAuctionQueues(pvfReady bool, catalog map[uint32]catalogItem) error {
	candidates, err := a.auctionQueueCandidates(pvfReady, catalog)
	if err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if len(a.auctionQueue) != 0 || len(a.auctionSpecialQueue) != 0 {
		if candidates.Source != marketQueueSourcePVFItemInfo || a.auctionQueueSource == marketQueueSourcePVFItemInfo {
			return nil
		}
	}
	a.applyAuctionQueueCandidatesLocked(candidates)
	return nil
}

func (a *App) applyAuctionQueueCandidatesLocked(candidates auctionQueueCandidatesResult) {
	candidateSet := idSet(append(append([]uint32{}, candidates.Normal...), candidates.Special...))
	a.auctionRejected = filterQueueBySet(a.auctionRejected, candidateSet)
	a.pruneAuctionRejectedMetaLocked()
	rejectedSet := idSet(a.auctionRejected)
	a.auctionQueue = filterQueueExcludeSet(candidates.Normal, rejectedSet)
	a.auctionSpecialQueue = filterQueueExcludeSet(candidates.Special, rejectedSet)
	a.auctionQueueSource = candidates.Source
}

func (a *App) selectAuctionQueueRowsLocked(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int, budget auctionQueueBudget) []restockRow {
	selected := make([]restockRow, 0)
	if maxActions <= 0 || budget.Special != 0 {
		selected = append(selected, a.selectAuctionRowsFromQueue(&a.auctionSpecialQueue, pvfReady, catalog, have, budget.Special, false)...)
	}
	selected = append(selected, a.selectAuctionRowsFromQueue(&a.auctionQueue, pvfReady, catalog, have, budget.Normal, false)...)
	if maxActions <= 0 || budget.Rejected != 0 {
		selected = append(selected, a.selectAuctionRowsFromQueue(&a.auctionRejected, pvfReady, catalog, have, budget.Rejected, true)...)
	}
	return selected
}

func (a *App) reconcileRejectedStockLocked(have map[uint32]int, pvfReady bool, catalog map[uint32]catalogItem) {
	if len(a.auctionRejected) == 0 || len(have) == 0 {
		return
	}
	out := a.auctionRejected[:0]
	for _, id := range a.auctionRejected {
		if have[id] > 0 {
			a.appendAuctionAvailableLocked(id, pvfReady, catalog)
			delete(a.auctionRejectedMeta, id)
			continue
		}
		out = append(out, id)
	}
	a.auctionRejected = out
}

func (a *App) auctionQueueBudgetLocked(maxActions int) auctionQueueBudget {
	if maxActions <= 0 {
		return auctionQueueBudget{}
	}
	rejected := 0
	if len(a.auctionRejected) == 0 {
		a.auctionRejectedTick = 0
	} else {
		a.auctionRejectedTick++
		if a.auctionRejectedTick >= auctionRejectedRetryEvery {
			a.auctionRejectedTick = 0
			rejected = maxActions / auctionRejectedRetryDivisor
			if rejected <= 0 {
				rejected = 1
			}
			if rejected > maxActions {
				rejected = maxActions
			}
		}
	}
	available := maxActions - rejected
	special := 0
	if len(a.auctionSpecialQueue) > 0 && available > 1 {
		special = available / auctionSpecialBudgetDivisor
		if special <= 0 {
			special = 1
		}
	}
	return auctionQueueBudget{Normal: available - special, Special: special, Rejected: rejected}
}

func (a *App) selectAuctionRowsFromQueue(queue *[]uint32, pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int, rejected bool) []restockRow {
	queueLen := len(*queue)
	selected := make([]restockRow, 0)
	planned := 0
	for i := 0; i < queueLen; i++ {
		id := (*queue)[0]
		*queue = (*queue)[1:]
		if have[id] > 0 {
			if rejected {
				a.appendAuctionAvailableLocked(id, pvfReady, catalog)
			} else {
				*queue = append(*queue, id)
			}
			continue
		}
		row, ok := a.auctionRowForID(pvfReady, catalog, id)
		if !ok {
			continue
		}
		records := auctionTargetRecords(row)
		if maxActions > 0 && planned > 0 && planned+records > maxActions {
			*queue = append(*queue, id)
			continue
		}
		selected = append(selected, row)
		planned += records
		*queue = append(*queue, id)
		if maxActions > 0 && planned >= maxActions {
			break
		}
	}
	return selected
}

func (a *App) markAuctionExplicitRejected(itemID uint32) {
	a.markAuctionRejected(itemID, "explicit_rejected")
}

func (a *App) applyAuctionActionFeedback(entry ActionEntry, err error) {
	if entry.Action.Market != marketNameAuction || entry.Action.Operation == "collect" || entry.Action.ItemID == 0 {
		return
	}
	if err == nil && entry.OK {
		return
	}
	a.markAuctionRejected(entry.Action.ItemID, auctionRejectionReason(entry, err))
}

func (a *App) markAuctionRejected(itemID uint32, reason string) {
	if itemID == 0 {
		return
	}
	if reason == "" {
		reason = "auction_rejected"
	}
	now := time.Now()
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.auctionQueue = removeQueueID(a.auctionQueue, itemID)
	a.auctionSpecialQueue = removeQueueID(a.auctionSpecialQueue, itemID)
	if !queueContains(a.auctionRejected, itemID) {
		a.auctionRejected = append(a.auctionRejected, itemID)
	}
	if a.auctionRejectedMeta == nil {
		a.auctionRejectedMeta = map[uint32]auctionRejectedState{}
	}
	state := a.auctionRejectedMeta[itemID]
	if state.Count == 0 {
		state.First = now
	}
	state.Last = now
	state.Count++
	state.Reason = reason
	a.auctionRejectedMeta[itemID] = state
}

func (a *App) appendAuctionAvailableLocked(itemID uint32, pvfReady bool, catalog map[uint32]catalogItem) {
	if itemID == 0 {
		return
	}
	delete(a.auctionRejectedMeta, itemID)
	if pvfReady {
		if item, ok := catalog[itemID]; ok && specialAuctionKind(item) != "" {
			if !queueContains(a.auctionSpecialQueue, itemID) {
				a.auctionSpecialQueue = append(a.auctionSpecialQueue, itemID)
			}
			return
		}
	}
	if !queueContains(a.auctionQueue, itemID) {
		a.auctionQueue = append(a.auctionQueue, itemID)
	}
}

func (a *App) pruneAuctionRejectedMetaLocked() {
	if len(a.auctionRejectedMeta) == 0 {
		return
	}
	keep := idSet(a.auctionRejected)
	for id := range a.auctionRejectedMeta {
		if !keep[id] {
			delete(a.auctionRejectedMeta, id)
		}
	}
}

func idSet(ids []uint32) map[uint32]bool {
	out := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			out[id] = true
		}
	}
	return out
}

func filterQueueBySet(ids []uint32, keep map[uint32]bool) []uint32 {
	out := ids[:0]
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && keep[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func filterQueueExcludeSet(ids []uint32, exclude map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(ids))
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && !exclude[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func removeQueueID(ids []uint32, itemID uint32) []uint32 {
	out := ids[:0]
	for _, id := range ids {
		if id != itemID {
			out = append(out, id)
		}
	}
	return out
}

func queueContains(ids []uint32, itemID uint32) bool {
	for _, id := range ids {
		if id == itemID {
			return true
		}
	}
	return false
}

func (a *App) auctionQueueCandidates(pvfReady bool, catalog map[uint32]catalogItem) (auctionQueueCandidatesResult, error) {
	if pvfReady {
		itemInfoIDs, path, err := a.currentItemInfoIDs()
		if err != nil {
			a.appendLog(LogEvent{Type: "iteminfo_gate", Status: marketLogStatusBlocked, Message: err.Error()})
			return auctionQueueCandidatesResult{Source: marketQueueSourcePVFItemInfoMissing}, nil
		}
		normal, special := catalogAuctionIDsByType(catalog, itemInfoIDs)
		a.appendLog(LogEvent{Type: "iteminfo_gate", Status: marketLogStatusActive, Message: fmt.Sprintf("source=%s allowed=%d special=%d", path, len(normal)+len(special), len(special))})
		return auctionQueueCandidatesResult{Normal: normal, Special: special, Source: marketQueueSourcePVFItemInfo}, nil
	}
	rows, err := a.fallbackAuctionRows()
	if err != nil {
		return auctionQueueCandidatesResult{}, err
	}
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		if row.ItemID != 0 {
			ids = append(ids, row.ItemID)
		}
	}
	return auctionQueueCandidatesResult{Normal: ids, Source: marketQueueSourceFallback}, nil
}

func (a *App) auctionRowForID(pvfReady bool, catalog map[uint32]catalogItem, id uint32) (restockRow, bool) {
	if pvfReady {
		item, ok := catalog[id]
		if !ok {
			return restockRow{}, false
		}
		return a.catalogAuctionRow(item)
	}
	rows, err := a.fallbackAuctionRows()
	if err != nil {
		return restockRow{}, false
	}
	for _, row := range rows {
		if row.ItemID == id {
			return row, true
		}
	}
	return restockRow{}, false
}

func (a *App) catalogAuctionRows(catalog map[uint32]catalogItem) []restockRow {
	ids := catalogAuctionIDs(catalog, nil)
	rows := make([]restockRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := a.catalogAuctionRow(catalog[id]); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func catalogAuctionIDs(catalog map[uint32]catalogItem, allowed map[uint32]bool) []uint32 {
	normal, special := catalogAuctionIDsByType(catalog, allowed)
	return append(normal, special...)
}

func catalogAuctionIDsByType(catalog map[uint32]catalogItem, allowed map[uint32]bool) ([]uint32, []uint32) {
	ids := make([]uint32, 0, len(catalog))
	special := make([]uint32, 0)
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		if !marketCandidate(item) {
			continue
		}
		if specialAuctionKind(item) != "" {
			special = append(special, id)
		} else {
			ids = append(ids, id)
		}
	}
	sortCatalogAuctionIDs(ids, catalog)
	sortCatalogSpecialAuctionIDs(special, catalog)
	return ids, special
}

func sortCatalogAuctionIDs(ids []uint32, catalog map[uint32]catalogItem) {
	sort.Slice(ids, func(i, j int) bool {
		left := catalog[ids[i]]
		right := catalog[ids[j]]
		if left.Kind != right.Kind {
			return left.Kind == "equipment"
		}
		if left.Kind == "equipment" && left.Level != right.Level {
			return left.Level > right.Level
		}
		return left.ItemID < right.ItemID
	})
}

func sortCatalogSpecialAuctionIDs(ids []uint32, catalog map[uint32]catalogItem) {
	sort.Slice(ids, func(i, j int) bool {
		left := catalog[ids[i]]
		right := catalog[ids[j]]
		leftRank := specialAuctionRank(left)
		rightRank := specialAuctionRank(right)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Level != right.Level {
			return left.Level > right.Level
		}
		return left.ItemID < right.ItemID
	})
}

func auctionTargetRecords(row restockRow) int {
	stackSize := row.StackSize
	if stackSize <= 0 {
		stackSize = 1
	}
	if row.Kind == "equipment" {
		stackSize = 1
	}
	return (row.Quantity + stackSize - 1) / stackSize
}

func (a *App) fallbackAuctionRows() ([]restockRow, error) {
	data, err := seedFiles.ReadFile("seeds/market_fallback_seed.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded market fallback: %w", err)
	}
	var seed fallbackSeed
	if err := json.Unmarshal(data, &seed); err != nil {
		return nil, fmt.Errorf("parse embedded market fallback: %w", err)
	}
	rows := make([]restockRow, 0, len(seed.Core))
	for _, item := range seed.Core {
		if item.ItemID == 0 || item.BasePrice <= 0 {
			continue
		}
		stack := a.randomStackSize(catalogItem{ItemID: item.ItemID, Kind: "stackable"})
		rows = append(rows, restockRow{
			ItemID:      item.ItemID,
			SystemPrice: item.BasePrice,
			Quantity:    stack,
			StackSize:   stack,
			Enabled:     true,
			Source:      marketRowSourceFallbackSeed,
			Kind:        "stackable",
		})
	}
	return rows, nil
}

func (a *App) catalogAuctionRow(item catalogItem) (restockRow, bool) {
	if !marketCandidate(item) {
		return restockRow{}, false
	}
	row := restockRow{
		ItemID:      item.ItemID,
		SystemPrice: marketBasePrice(item),
		Enabled:     true,
		Source:      marketRowSourcePVF,
		Kind:        item.Kind,
		Level:       item.Level,
		ItemType:    item.ItemType,
		SubType:     item.SubType,
		Slot:        item.Slot,
		Attach:      item.Attach,
		Rarity:      item.Rarity,
		StackLimit:  item.StackLimit,
	}
	if item.Kind == "equipment" {
		row.Quantity = randRange(a.rand, a.cfg.Restock.EquipmentQtyMin, a.cfg.Restock.EquipmentQtyMax)
		row.StackSize = 1
	} else {
		stack := a.randomStackSize(item)
		row.Quantity = stack
		row.StackSize = stack
	}
	return row, true
}

func (a *App) randomStackSize(item catalogItem) int {
	sizes := a.cfg.Restock.StackSizes
	if len(sizes) == 0 {
		sizes = DefaultConfig().Restock.StackSizes
	}
	stack := sizes[a.rand.Intn(len(sizes))]
	if item.StackLimit > 0 && stack > item.StackLimit {
		stack = item.StackLimit
	}
	if stack <= 0 {
		stack = 1
	}
	return stack
}

func randRange(rng *rand.Rand, min, max int) int {
	if min <= 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min + rng.Intn(max-min+1)
}

func mergeStringMap(dst *map[string]string, defaults map[string]string) {
	if *dst == nil {
		*dst = map[string]string{}
	}
	for key, value := range defaults {
		(*dst)[key] = value
	}
}

func defaultRestockComments() map[string]string {
	return map[string]string{
		"_summary":              "Normal auction uses PVF for item data and the current auction iteminfo.dat as the environment boundary. Candidate IDs are PVF auctionable IDs intersected with iteminfo.dat IDs; clicking ItemInfo explicitly releases the generated compatible dat and expands that boundary.",
		"stack_sizes":           "Stackable listing count candidates, such as material bundles. The selected count is clamped by PVF stack_limit when available.",
		"equipment_qty_min":     "Minimum duplicate records generated for each missing equipment item after the DB shows no system stock for that item.",
		"equipment_qty_max":     "Maximum duplicate records generated for each missing equipment item after the DB shows no system stock for that item.",
		"equipment_inflate_min": "Lower equipment base price multiplier. PVF price/value remains the base.",
		"equipment_inflate_max": "Upper equipment base price multiplier. PVF price/value remains the base.",
		"upgrade_min":           "Minimum random equipment upgrade value written to the auction packet.",
		"upgrade_max":           "Maximum random equipment upgrade value written to the auction packet.",
		"upgrade_price_rate":    "Additional equipment price rate per upgrade level.",
		"rand_low":              "Final random price multiplier lower bound for both stackable and equipment listings.",
		"rand_high":             "Final random price multiplier upper bound for both stackable and equipment listings.",
		"max_actions":           "Maximum register packets per restock round. Default is 10000; use 0 only when a caller intentionally wants the full DB gap.",
		"max_concurrent":        "Concurrent auction register workers. This controls send pressure, not item selection.",
		"max_result_actions":    "Maximum action details retained in job result to keep UI/log payload bounded.",
		"per_item_delay_ms":     "Optional delay between actions in each worker. 0 means no intentional delay.",
	}
}
func defaultCeraComments() map[string]string {
	return map[string]string{
		"_summary":      "Gold consignment uses the fixed item list below. It is separate from normal auction item selection and does not use the PVF/iteminfo intersection gate.",
		"items":         "Gold package list. Entries with enabled=false are kept in config but not restocked.",
		"item_id":       "Gold package item ID.",
		"name":          "Display label used only for identification in config and logs.",
		"restock_price": "Consignment listing price.",
		"restock_qty":   "Target record count. Restock fills the gap when current DB stock is lower than this value.",
		"recycle_price": "Reserved reference price for future collect policy.",
		"enabled":       "Whether this gold package is enabled.",
	}
}
func defaultCeraRows() []ceraRow {
	return []ceraRow{
		{ItemID: 2675336, Label: "100w_gold", RestockPrice: 200, RestockQty: 20, RecyclePrice: 200, Enabled: true},
		{ItemID: 2675337, Label: "200w_gold", RestockPrice: 400, RestockQty: 20, RecyclePrice: 400, Enabled: true},
		{ItemID: 2675338, Label: "300w_gold", RestockPrice: 600, RestockQty: 20, RecyclePrice: 600, Enabled: true},
		{ItemID: 2675339, Label: "400w_gold", RestockPrice: 800, RestockQty: 20, RecyclePrice: 800, Enabled: true},
		{ItemID: 2675340, Label: "500w_gold", RestockPrice: 1000, RestockQty: 20, RecyclePrice: 1000, Enabled: true},
		{ItemID: 2675341, Label: "600w_gold", RestockPrice: 1200, RestockQty: 20, RecyclePrice: 1200, Enabled: true},
		{ItemID: 2675342, Label: "700w_gold", RestockPrice: 1400, RestockQty: 20, RecyclePrice: 1400, Enabled: true},
		{ItemID: 2675343, Label: "800w_gold", RestockPrice: 1600, RestockQty: 20, RecyclePrice: 1600, Enabled: true},
		{ItemID: 2675344, Label: "900w_gold", RestockPrice: 1800, RestockQty: 20, RecyclePrice: 1800, Enabled: true},
		{ItemID: 2675345, Label: "1000w_gold", RestockPrice: 2000, RestockQty: 20, RecyclePrice: 2000, Enabled: true},
		{ItemID: 2675346, Label: "2000w_gold", RestockPrice: 4000, RestockQty: 20, RecyclePrice: 4000, Enabled: true},
		{ItemID: 2675347, Label: "3000w_gold", RestockPrice: 6000, RestockQty: 20, RecyclePrice: 6000, Enabled: true},
	}
}

// ---- workers.go ----
type actionTask struct {
	index  int
	action Action
}

type actionLogAccumulator struct {
	total       int
	ok          int
	failed      int
	errorCount  int
	byMarket    map[string]int
	byReason    map[string]int
	failedItems map[uint32]actionLogFailedItem
}

type actionLogFailedItem struct {
	count  int
	reason string
}

func newActionLogAccumulator() actionLogAccumulator {
	return actionLogAccumulator{
		byMarket:    map[string]int{},
		byReason:    map[string]int{},
		failedItems: map[uint32]actionLogFailedItem{},
	}
}

func (s *actionLogAccumulator) add(entry ActionEntry, err error) {
	s.total++
	s.byMarket[entry.Action.Market]++
	if err == nil && entry.OK {
		s.ok++
		return
	}
	s.failed++
	reason := actionLogReason(entry, err)
	s.byReason[reason]++
	if err != nil {
		s.errorCount++
	}
	if entry.Action.ItemID != 0 {
		item := s.failedItems[entry.Action.ItemID]
		item.count++
		if item.reason == "" {
			item.reason = reason
		}
		s.failedItems[entry.Action.ItemID] = item
	}
}

func (s actionLogAccumulator) summary() ActionLogSummary {
	return ActionLogSummary{
		Total:      s.total,
		OK:         s.ok,
		Failed:     s.failed,
		ErrorCount: s.errorCount,
		ByMarket:   compactCountMap(s.byMarket),
		ByReason:   compactCountMap(s.byReason),
		TopFailed:  topActionLogFailedItems(s.failedItems, 20),
	}
}

func actionLogReason(entry ActionEntry, err error) string {
	if err != nil {
		return "executor_error"
	}
	if entry.Reason != nil {
		return fmt.Sprintf("%d", *entry.Reason)
	}
	if entry.Action.Operation != "collect" && entry.AuctionID == 0 {
		return "missing_auction_id"
	}
	return "rejected"
}

func auctionRejectionReason(entry ActionEntry, err error) string {
	return actionLogReason(entry, err)
}

func compactCountMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		if k != "" && v > 0 {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func topActionLogFailedItems(in map[uint32]actionLogFailedItem, limit int) []ActionLogItem {
	if len(in) == 0 || limit <= 0 {
		return nil
	}
	items := make([]ActionLogItem, 0, len(in))
	for id, stat := range in {
		items = append(items, ActionLogItem{ItemID: id, Count: stat.count, Reason: stat.reason})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].ItemID < items[j].ItemID
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (a *App) executeActions(jobID string, actions []Action, maxConcurrent int, continueOnError bool, job *JobSummary) (int, []ActionEntry, error) {
	if len(actions) == 0 {
		return 0, nil, nil
	}
	workers := maxConcurrent
	if workers <= 0 {
		workers = a.cfg.Restock.MaxConcurrent
	}
	if workers <= 0 {
		workers = 32
	}
	if workers > len(actions) {
		workers = len(actions)
	}
	delay := time.Duration(a.cfg.Restock.PerItemDelayMS) * time.Millisecond
	resultLimit := a.cfg.Restock.MaxResultActions
	if resultLimit <= 0 {
		resultLimit = 200
	}

	tasks := make(chan actionTask)
	stop := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	var mu lockhub.Locker
	failed := 0
	entries := make([]ActionEntry, 0, len(actions))
	actionLog := newActionLogAccumulator()
	var firstErr error

	record := func(entry ActionEntry, err error) {
		a.applyAuctionActionFeedback(entry, err)
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, entry)
		actionLog.add(entry, err)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		} else if !entry.OK {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("action rejected reason=%v", byteValue(entry.Reason))
			}
		}
		if len(job.Actions) < resultLimit {
			job.Actions = append(job.Actions, entry)
		}
		if !continueOnError && firstErr != nil {
			stopOnce.Do(func() { close(stop) })
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			executor := a.executors.NewActionExecutor(a.cfg)
			defer executor.Close()
			for task := range tasks {
				select {
				case <-stop:
					return
				default:
				}
				entry := ActionEntry{Index: task.index, Action: task.action}
				res, err := executor.Execute(task.action)
				if err != nil {
					entry.Error = err.Error()
					record(entry, err)
				} else {
					entry.OK = res.ResultOK != nil && *res.ResultOK
					entry.AuctionID = res.AuctionID
					if task.action.Market == marketNameAuction && task.action.Operation != "collect" && entry.AuctionID == 0 {
						entry.OK = false
					}
					entry.Reason = res.ResultReason
					entry.Result = res.Raw
					record(entry, nil)
				}
				if delay > 0 {
					select {
					case <-time.After(delay):
					case <-stop:
						return
					}
				}
			}
		}()
	}

sendLoop:
	for i, action := range actions {
		select {
		case <-stop:
			break sendLoop
		case tasks <- actionTask{index: i, action: action}:
		}
		select {
		case <-stop:
			break sendLoop
		default:
		}
	}
	close(tasks)
	wg.Wait()
	summary := actionLog.summary()
	a.appendLog(LogEvent{Type: "action_summary", JobID: jobID, ActionSummary: &summary})
	return failed, entries, firstErr
}
