package marketapp

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const marketTableRetryInterval = time.Minute

func (a *App) StartAuto() {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoRun || a.autoShutdown {
		return
	}
	a.startAutoLocked()
}

func (a *App) startAutoLocked() {
	ctx, cancel := context.WithCancel(a.lifecycleContextLocked())
	a.autoDone = make(chan struct{})
	a.autoCtx = ctx
	a.autoCancel = cancel
	a.autoRun = true
	a.autoStop = false
	a.autoRestart = false
	go a.autoLoop(ctx, a.autoDone)
}

func (a *App) StopAuto() {
	a.stopAutoWithWait(true)
}

func (a *App) StopAutoAsync() {
	a.stopAutoWithWait(false)
}

func (a *App) RestartAutoAsync() {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoShutdown {
		return
	}
	if !a.autoRun {
		a.startAutoLocked()
		return
	}
	a.autoRestart = true
	a.cancelAutoLocked()
}

func (a *App) startAutoIfEnabled() {
	if a.configSnapshot().Auto.Enabled {
		a.StartAuto()
	}
}

func (a *App) stopAutoWithWait(wait bool) {
	a.autoMu.Lock()
	a.autoRestart = false
	if !a.autoRun {
		a.autoMu.Unlock()
		return
	}
	a.cancelAutoLocked()
	done := a.autoDone
	a.autoMu.Unlock()
	if wait {
		<-done
	}
}

func (a *App) Shutdown() {
	a.autoMu.Lock()
	a.autoShutdown = true
	a.autoRestart = false
	if a.lifecycleCancel != nil {
		a.lifecycleCancel()
	}
	var done chan struct{}
	if a.autoRun {
		a.cancelAutoLocked()
		done = a.autoDone
	}
	a.autoMu.Unlock()
	if done != nil {
		<-done
	}
	a.logMu.Lock()
	a.logClosed = true
	if a.logWriter != nil {
		_ = a.logWriter.Close()
		a.logWriter = nil
	}
	a.logMu.Unlock()
}

func (a *App) finishAutoLoop(done chan struct{}) {
	a.autoMu.Lock()
	if a.autoDone != done {
		a.autoMu.Unlock()
		return
	}
	a.autoRun = false
	a.autoStop = false
	a.autoCtx = nil
	a.autoCancel = nil
	restart := a.autoRestart && !a.autoShutdown
	a.autoRestart = false
	close(done)
	if restart {
		a.startAutoLocked()
	}
	a.autoMu.Unlock()
}

func (a *App) AutoRunning() bool {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return a.autoRun
}

func (a *App) autoLoop(ctx context.Context, done chan struct{}) {
	defer a.finishAutoLoop(done)
	cfg := a.configSnapshot()
	if !cfg.Auto.Enabled {
		a.appendLog(LogEvent{Type: "auto", Status: marketLogStatusDisabled})
		return
	}
	initial := time.Duration(cfg.Auto.InitialDelayMS) * time.Millisecond
	if initial > 0 {
		select {
		case <-time.After(initial):
		case <-ctx.Done():
			return
		}
	}
	a.runAutoOnceSafely(ctx)
	if ctx.Err() != nil {
		return
	}
	interval := time.Duration(cfg.Auto.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.runAutoOnceSafely(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (a *App) runAutoOnceSafely(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[MarketAuto] run panic err=%v\n", rec)
		}
	}()
	a.runAutoOnce(ctx)
}

func (a *App) runAutoOnce(ctx context.Context) {
	cfg := a.configSnapshot()
	a.ensureMarketTables(time.Now())
	markets := cfg.Auto.Markets
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
		policy := a.marketAutoPolicy(market, cfg.Auto)
		if cfg.Collector.Enabled {
			a.appendLog(LogEvent{Type: "auto_collect", Market: market, Status: marketLogStatusStart})
			job, err := a.collectOnce(ctx, CollectRequest{
				Market:          market,
				Execute:         true,
				MaxActions:      policy.MaxActions,
				MaxConcurrent:   policy.MaxConcurrent,
				ContinueOnError: cfg.Auto.ContinueOnError,
			})
			status := job.Status
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			a.appendLog(LogEvent{Type: "auto_collect", JobID: job.ID, Market: market, Status: status, Message: msg})
		}
		a.appendLog(LogEvent{Type: "auto_run", Market: market, Status: marketLogStatusStart})
		job, err := a.restockOnce(ctx, RestockRequest{
			Market:          market,
			Execute:         true,
			MaxActions:      policy.MaxActions,
			MaxConcurrent:   policy.MaxConcurrent,
			ContinueOnError: cfg.Auto.ContinueOnError,
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

func (a *App) cancelAutoLocked() {
	if a.autoStop {
		return
	}
	a.autoStop = true
	if a.autoCancel != nil {
		a.autoCancel()
	}
}

func (a *App) lifecycleContext() context.Context {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return a.lifecycleContextLocked()
}

func (a *App) lifecycleContextLocked() context.Context {
	if a.lifecycleCtx == nil {
		a.lifecycleCtx, a.lifecycleCancel = context.WithCancel(context.Background())
	}
	return a.lifecycleCtx
}

// ensureMarketTables performs schema preparation once after a successful
// initialization. Failures are retried with a bounded interval instead of
// issuing DDL on every automatic market cycle.
func (a *App) ensureMarketTables(now time.Time) {
	if a == nil || a.repository == nil {
		return
	}
	a.stateMu.Lock()
	if a.dbGeneration == 0 {
		a.dbGeneration = 1
	}
	generation := a.dbGeneration
	dbNames := []string{a.cfg.AuctionDB, a.cfg.CeraDB}
	if a.dbInitOK || !a.dbRetryAt.IsZero() && now.Before(a.dbRetryAt) {
		a.stateMu.Unlock()
		return
	}
	a.dbRetryAt = now.Add(marketTableRetryInterval)
	a.stateMu.Unlock()

	tables, err := a.repository.EnsureMarketTables(dbNames, now)
	a.stateMu.Lock()
	if generation != a.dbGeneration {
		a.stateMu.Unlock()
		return
	}
	a.dbInit = tables
	a.dbInitOK = err == nil
	if err != nil {
		a.dbInitErr = err.Error()
	} else {
		a.dbInitErr = ""
		a.dbRetryAt = time.Time{}
	}
	a.stateMu.Unlock()
	if err != nil {
		a.appendLog(LogEvent{Type: "db_init", Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	a.appendLog(LogEvent{Type: "db_init", Status: marketLogStatusSuccess, Message: strings.Join(tables, ",")})
}
