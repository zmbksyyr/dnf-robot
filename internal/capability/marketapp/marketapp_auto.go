package marketapp

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"robot/internal/foundation/logfile"
)

const marketServiceRestartCooldown = 10 * time.Minute

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
	if ready[marketServiceNameAuction] {
		if result, err := a.patchAuctionMemory(); err != nil {
			a.appendLog(LogEvent{Type: "auction_memory_patch", Status: marketLogStatusFailed, Message: err.Error()})
		} else {
			a.logAuctionMemoryPatchResult(result, false)
		}
	}
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
		for _, service := range a.marketServiceSpecs() {
			needed[service.name] = true
		}
	}
	for _, service := range a.marketServiceSpecs() {
		if needed[service.name] {
			ready[service.name] = a.ensureMarketService(service)
		}
	}
	return ready
}

func (a *App) ensureRunningMarketServiceLogSinks() {
	if runtime.GOOS != "linux" {
		return
	}
	for _, service := range a.marketServiceSpecs() {
		if tcpReady(service.addr, 500*time.Millisecond) {
			a.ensureMarketService(service)
		}
	}
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
	return a.ensureMarketServiceOnce(service, false)
}

func (a *App) ensureMarketServiceOnce(service marketServiceSpec, recovered bool) bool {
	status := MarketServiceStatus{Name: service.name, Addr: service.addr, Dir: service.dir, Bin: service.bin, CheckedAt: time.Now(), LogPath: a.marketServiceLogPath(service.name)}
	maxBytes, backups := a.logLimits()
	sinkBin := ""
	if runtime.GOOS == "linux" {
		var err error
		sinkBin, err = os.Executable()
		if err != nil {
			status.Status = MarketServiceStatusStartFailed
			status.Message = fmt.Sprintf("resolve bounded log sink: %v", err)
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
			return false
		}
	}
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
		} else if sinkBin != "" && !marketServiceLogSinkRunning(sinkBin, status.LogPath, maxBytes, backups) {
			status.Status = MarketServiceStatusDown
			status.Message = "service log sink is missing"
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
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
	cmdline := marketServiceShellCommand(service.bin, service.args, status.LogPath, sinkBin, maxBytes, backups)
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
			if a.hasMarketServiceFailure(status.LogPath) {
				status.Status = MarketServiceStatusRegistItemFailed
				status.Message = "service log contains RegistItem failure"
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				if !recovered && service.name == marketServiceNameAuction && a.recoverAuctionRegistItemFailure(status.LogPath) {
					return a.ensureMarketServiceOnce(service, true)
				}
				return false
			}
			if sinkBin != "" && !marketServiceLogSinkRunning(sinkBin, status.LogPath, maxBytes, backups) {
				status.Status = MarketServiceStatusProcessExited
				status.Message = "bounded log sink exited during startup stability window"
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
	if !recovered && service.name == marketServiceNameAuction && a.hasMarketServiceFailure(status.LogPath) && a.recoverAuctionRegistItemFailure(status.LogPath) {
		return a.ensureMarketServiceOnce(service, true)
	}
	return false
}

func (a *App) recoverAuctionRegistItemFailure(logPath string) bool {
	deleted, err := a.repository.DeleteSystemStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase)
	if err != nil {
		a.appendLog(LogEvent{Type: "market_service", Market: marketServiceNameAuction, Status: marketLogStatusFailed, Message: fmt.Sprintf("regist item recovery clear failed: %v", err)})
		return false
	}
	a.resetAuctionQueues()
	a.appendLog(LogEvent{Type: "market_service", Market: marketServiceNameAuction, Status: marketLogStatusDBDeleted, Message: fmt.Sprintf("regist item recovery cleared auction rows=%d log=%s", deleted, logPath)})
	return deleted > 0
}

func (a *App) refreshMarketServiceStatuses() {
	for _, service := range a.marketServiceSpecs() {
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

func (a *App) marketServiceSpecs() []marketServiceSpec {
	auctionPort := a.cfg.AuctionPort
	if auctionPort <= 0 {
		auctionPort = 30803
	}
	pointPort := a.cfg.CeraPort
	if pointPort <= 0 {
		pointPort = 30603
	}
	return []marketServiceSpec{
		{name: marketServiceNameAuction, addr: fmt.Sprintf("127.0.0.1:%d", auctionPort), dir: "/home/neople/auction", bin: "./df_auction_r", args: []string{"./cfg/auction_cain.cfg", "start", "./df_auction_r"}},
		{name: marketServiceNamePoint, addr: fmt.Sprintf("127.0.0.1:%d", pointPort), dir: "/home/neople/point", bin: "./df_point_r", args: []string{"./cfg/point_cain.cfg", "start", "df_point_r"}},
	}
}

func (a *App) marketServiceSpecByName(name string) (marketServiceSpec, bool) {
	for _, service := range a.marketServiceSpecs() {
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
	for _, service := range a.marketServiceSpecs() {
		if err := a.restartMarketServiceAfterItemInfo(service); err != nil {
			return err
		}
	}
	a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusSuccess, Message: "auction and point services restarted"})
	return nil
}

func (a *App) restartMarketServiceAfterItemInfo(service marketServiceSpec) error {
	if err := a.stopMarketServiceForItemInfo(service.name, service.addr, service.bin); err != nil {
		return err
	}
	if !a.ensureMarketService(service) {
		return fmt.Errorf("%s restart failed: service is not ready", service.name)
	}
	return nil
}

func (a *App) stopMarketServiceForItemInfo(name, addr, bin string) error {
	process := filepath.Base(strings.TrimSpace(bin))
	if process == "" || process == "." || process == "/" {
		return fmt.Errorf("%s stop failed: invalid process name %q", name, bin)
	}
	pid := marketServicePID(bin)
	if pid <= 0 && !tcpReady(addr, 200*time.Millisecond) {
		if err := a.stopMarketServiceLogSink(name); err != nil {
			return err
		}
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopSkipped, Message: "process and port are already down"})
		return nil
	}
	_ = exec.Command("pkill", "-TERM", "-x", process).Run()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			if err := a.stopMarketServiceLogSink(name); err != nil {
				return err
			}
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopped, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-x", process).Run()
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			if err := a.stopMarketServiceLogSink(name); err != nil {
				return err
			}
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusKilled, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s stop timeout: %s still running or port still listening", name, process)
}

func (a *App) stopMarketServiceLogSink(name string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	sinkBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve bounded log sink: %w", err)
	}
	pattern := "^" + regexp.QuoteMeta(sinkBin) + " --bounded-log-sink " + regexp.QuoteMeta(a.marketServiceLogPath(name)) + "( |$)"
	_ = exec.Command("pkill", "-TERM", "-f", pattern).Run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("pgrep", "-f", pattern).Output()
		if len(strings.Fields(string(out))) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-f", pattern).Run()
	out, _ := exec.Command("pgrep", "-f", pattern).Output()
	if len(strings.Fields(string(out))) > 0 {
		return fmt.Errorf("%s bounded log sink did not stop", name)
	}
	return nil
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

func marketServiceLogSinkRunning(sinkBin, outputPath string, maxBytes int64, backups int) bool {
	if runtime.GOOS != "linux" || strings.TrimSpace(sinkBin) == "" {
		return true
	}
	pattern := "^" + regexp.QuoteMeta(sinkBin) + " --bounded-log-sink " + regexp.QuoteMeta(outputPath) +
		" --bounded-log-max-bytes " + strconv.FormatInt(maxBytes, 10) +
		" --bounded-log-backups " + strconv.Itoa(backups) + "$"
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	return err == nil && len(strings.Fields(string(out))) > 0
}

func (a *App) hasMarketServiceFailure(logPath string) bool {
	maxBytes, _ := a.logLimits()
	found, err := logfile.ContainsAnyTail(logPath, maxBytes,
		"fail to registitem",
		"process exits",
		"fatal",
	)
	return err == nil && found
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

func marketServiceShellCommand(bin string, args []string, outputPath, sinkBin string, maxBytes int64, backups int) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, shellQuote(bin))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	if outputPath == "" {
		outputPath = "/dev/null"
	}
	quotedOutput := shellQuote(outputPath)
	service := strings.Join(parts, " ")
	if strings.TrimSpace(sinkBin) == "" {
		return ": >" + quotedOutput + "; nohup " + service + " >>" + quotedOutput + " 2>&1 &"
	}
	sink := shellQuote(sinkBin) + " --bounded-log-sink " + quotedOutput +
		" --bounded-log-max-bytes " + strconv.FormatInt(maxBytes, 10) +
		" --bounded-log-backups " + strconv.Itoa(backups)
	pipeline := service + " 2>&1 | " + sink
	return ": >" + quotedOutput + "; nohup sh -c " + shellQuote(pipeline) + " >/dev/null 2>&1 &"
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

func (a *App) restartMarketService(name, reason string) {
	if !a.allowMarketServiceRestart(name, reason) {
		return
	}
	if a.restarter != nil {
		a.restarter(name, reason)
		return
	}
	service, ok := a.marketServiceSpecByName(name)
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
