package marketapp

import (
	"strings"
	"time"
)

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
