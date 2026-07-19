package marketapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const marketServiceRestartCooldown = 10 * time.Minute

type marketServiceSpec struct {
	name string
	addr string
	dir  string
	bin  string
	args []string
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

func (a *App) setMarketServiceStatus(status MarketServiceStatus) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.services == nil {
		a.services = map[string]MarketServiceStatus{}
	}
	a.services[status.Name] = status
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
