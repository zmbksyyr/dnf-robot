package marketapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"robot/internal/foundation/config"
)

const (
	marketServiceRestartCooldown = 10 * time.Minute
	marketServiceStatusCacheTTL  = 2 * time.Second
)

const (
	marketServiceStabilitySamples  = 3
	marketServiceStabilityInterval = 500 * time.Millisecond
	marketServiceStabilityTimeout  = 4 * time.Second
)

type marketServiceSpec struct {
	name      string
	addr      string
	dir       string
	bin       string
	args      []string
	source    string
	launchErr error
}

func (a *App) ensureMarketServices(markets []string) map[string]bool {
	a.serviceMu.Lock()
	defer a.serviceMu.Unlock()
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
	services := make([]marketServiceSpec, 0, len(needed))
	for _, service := range a.marketServiceSpecs() {
		if needed[service.name] {
			services = append(services, service)
		}
	}
	return a.ensureMarketServiceSet(services)
}

func (a *App) ensureMarketServiceSet(services []marketServiceSpec) map[string]bool {
	type result struct {
		name  string
		ready bool
	}
	results := make(chan result, len(services))
	var wg sync.WaitGroup
	for _, service := range services {
		service := service
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready := false
			if err := runMarketServiceCall(service.name, "ensure", func() error {
				ready = a.ensureMarketService(service)
				return nil
			}); err != nil {
				fmt.Printf("[MarketService] %v\n", err)
			}
			results <- result{name: service.name, ready: ready}
		}()
	}
	wg.Wait()
	close(results)
	ready := make(map[string]bool, len(services))
	for item := range results {
		ready[item.name] = item.ready
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
	status := MarketServiceStatus{Name: service.name, Addr: service.addr, Dir: service.dir, Bin: service.bin, CheckedAt: time.Now()}
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
	if service.launchErr != nil {
		status.Status = MarketServiceStatusPrepareFailed
		status.Message = service.launchErr.Error()
		a.setMarketServiceStatus(status)
		a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
		return false
	}
	if err := validateMarketServiceItemInfo(a.itemInfoTargetForService(service.name)); err != nil {
		status.Status = MarketServiceStatusPrepareFailed
		status.Message = err.Error()
		a.setMarketServiceStatus(status)
		a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
		return false
	}
	if err := prepareMarketServiceDir(service.dir); err != nil {
		status.Status = MarketServiceStatusPrepareFailed
		status.Message = err.Error()
		a.setMarketServiceStatus(status)
		a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
		return false
	}
	status.StartedAt = time.Now()
	cmdline := marketServiceShellCommand(service.bin, service.args)
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
	a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: marketLogStatusStart, Message: fmt.Sprintf("addr=%s source=%s output=%s", service.addr, service.source, strings.TrimSpace(string(out)))})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if tcpReady(service.addr, 500*time.Millisecond) {
			status.Listening = true
			status.PID = marketServicePID(service.bin)
			var stable bool
			status.PID, status.Listening, stable = waitForMarketServiceStability(
				marketServiceStabilitySamples,
				marketServiceStabilityInterval,
				marketServiceStabilityTimeout,
				func() (int, bool) {
					return marketServicePID(service.bin), tcpReady(service.addr, 500*time.Millisecond)
				},
			)
			status.CheckedAt = time.Now()
			if stable {
				status.Status = MarketServiceStatusReady
				status.Message = service.addr
				a.setMarketServiceStatus(status)
				a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
				return true
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
			status.Status = MarketServiceStatusPortReadyButUnstable
			status.Message = "service did not remain stable during startup window"
			a.setMarketServiceStatus(status)
			a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
	status.Status = MarketServiceStatusStartTimeout
	status.Message = service.addr
	a.setMarketServiceStatus(status)
	a.appendLog(LogEvent{Type: "market_service", Market: service.name, Status: status.Status, Message: status.Message})
	return false
}

func waitForMarketServiceStability(samples int, interval, timeout time.Duration, probe func() (int, bool)) (int, bool, bool) {
	if samples < 1 {
		samples = 1
	}
	deadline := time.Now().Add(timeout)
	stable := 0
	lastPID := 0
	lastListening := false
	for {
		lastPID, lastListening = probe()
		if lastPID > 0 && lastListening {
			stable++
			if stable >= samples {
				return lastPID, lastListening, true
			}
		} else {
			stable = 0
		}
		if !time.Now().Before(deadline) {
			return lastPID, lastListening, false
		}
		if interval > 0 {
			time.Sleep(interval)
		}
	}
}

func validateMarketServiceItemInfo(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("iteminfo target is not configured")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("iteminfo target %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("iteminfo target %s is empty or not a regular file", path)
	}
	return nil
}

func (a *App) refreshMarketServiceStatuses() {
	a.serviceStatusMu.Lock()
	defer a.serviceStatusMu.Unlock()
	services := a.marketServiceSpecs()
	if a.marketServiceStatusesFresh(services, time.Now()) {
		return
	}
	var wg sync.WaitGroup
	for _, service := range services {
		service := service
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runMarketServiceCall(service.name, "refresh", func() error {
				a.refreshMarketServiceStatus(service)
				return nil
			}); err != nil {
				fmt.Printf("[MarketService] %v\n", err)
			}
		}()
	}
	wg.Wait()
}

func (a *App) marketServiceStatusesFresh(services []marketServiceSpec, now time.Time) bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	for _, service := range services {
		status, ok := a.services[service.name]
		if !ok || status.CheckedAt.IsZero() || now.Sub(status.CheckedAt) >= marketServiceStatusCacheTTL {
			return false
		}
	}
	return len(services) > 0
}

func (a *App) refreshMarketServiceStatus(service marketServiceSpec) {
	status := MarketServiceStatus{
		Name:      service.name,
		Addr:      service.addr,
		Dir:       service.dir,
		Bin:       service.bin,
		CheckedAt: time.Now(),
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
	preferredDir := filepath.Clean(filepath.Join(config.DNFServiceRoot(a.dfGameR), serviceName))
	fallback := ""
	for _, target := range a.configSnapshot().ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" || filepath.Base(filepath.Dir(target)) != serviceName {
			continue
		}
		if filepath.Clean(filepath.Dir(target)) == preferredDir {
			return target
		}
		if fallback == "" {
			fallback = target
		}
	}
	return fallback
}

func (a *App) marketServiceSpecs() []marketServiceSpec {
	a.serviceSpecMu.Lock()
	defer a.serviceSpecMu.Unlock()
	if a.serviceSpecs != nil {
		return append([]marketServiceSpec(nil), a.serviceSpecs...)
	}
	cfg := a.configSnapshot()
	auctionPort := cfg.AuctionPort
	if auctionPort <= 0 {
		auctionPort = 30803
	}
	pointPort := cfg.CeraPort
	if pointPort <= 0 {
		pointPort = 30603
	}
	root := config.DNFServiceRoot(a.dfGameR)
	auctionLaunch, auctionErr := a.discoverMarketServiceLaunch(marketServiceNameAuction, root)
	pointLaunch, pointErr := a.discoverMarketServiceLaunch(marketServiceNamePoint, root)
	a.serviceSpecs = []marketServiceSpec{
		{name: marketServiceNameAuction, addr: fmt.Sprintf("127.0.0.1:%d", auctionPort), dir: auctionLaunch.dir, bin: auctionLaunch.bin, args: auctionLaunch.args, source: auctionLaunch.source, launchErr: auctionErr},
		{name: marketServiceNamePoint, addr: fmt.Sprintf("127.0.0.1:%d", pointPort), dir: pointLaunch.dir, bin: pointLaunch.bin, args: pointLaunch.args, source: pointLaunch.source, launchErr: pointErr},
	}
	return append([]marketServiceSpec(nil), a.serviceSpecs...)
}

func (a *App) marketServiceSpecByName(name string) (marketServiceSpec, bool) {
	for _, service := range a.marketServiceSpecs() {
		if service.name == name {
			return service, true
		}
	}
	return marketServiceSpec{}, false
}

func (a *App) marketServiceRunningStates() map[string]bool {
	states := make(map[string]bool)
	for _, service := range a.marketServiceSpecs() {
		states[service.name] = marketServicePID(service.bin) > 0 || tcpReady(service.addr, 200*time.Millisecond)
	}
	return states
}

func marketServicesToRestore(services []marketServiceSpec, running map[string]bool) []marketServiceSpec {
	active := make([]marketServiceSpec, 0, len(services))
	for _, service := range services {
		if running[service.name] {
			active = append(active, service)
		}
	}
	return active
}

func (a *App) restartMarketServicesAfterItemInfo(running map[string]bool) error {
	if runtime.GOOS != "linux" {
		a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusSkipped, Message: "market service restart is linux only"})
		return nil
	}
	a.serviceMu.Lock()
	defer a.serviceMu.Unlock()
	allServices := a.marketServiceSpecs()
	services := marketServicesToRestore(allServices, running)
	for _, service := range allServices {
		if !running[service.name] {
			a.appendLog(LogEvent{Type: "iteminfo_restart", Market: service.name, Status: marketLogStatusSkipped, Message: "service was already stopped"})
		}
	}
	errorsByName := make(chan string, len(services))
	var wg sync.WaitGroup
	for _, service := range services {
		service := service
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runMarketServiceCall(service.name, "restart", func() error {
				return a.restartMarketServiceAfterItemInfo(service)
			}); err != nil {
				errorsByName <- fmt.Sprintf("%s: %v", service.name, err)
			}
		}()
	}
	wg.Wait()
	close(errorsByName)
	var failures []string
	for message := range errorsByName {
		failures = append(failures, message)
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("market service restart failed: %s", strings.Join(failures, "; "))
	}
	a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusSuccess, Message: fmt.Sprintf("market service state restored: restarted=%d kept_stopped=%d", len(services), len(allServices)-len(services))})
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

func runMarketServiceCall(name, operation string, call func() error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%s %s panic: %v", name, operation, rec)
		}
	}()
	if call == nil {
		return fmt.Errorf("%s %s callback is nil", name, operation)
	}
	return call()
}

func (a *App) restartMarketService(name, reason string) {
	if !a.allowMarketServiceRestart(name, reason) {
		return
	}
	a.serviceMu.Lock()
	defer a.serviceMu.Unlock()
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
