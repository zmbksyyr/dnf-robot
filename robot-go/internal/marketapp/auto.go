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
	go a.autoLoop()
}

func (a *App) StopAuto() {
	a.autoMu.Lock()
	if !a.autoRun {
		a.autoMu.Unlock()
		return
	}
	stop := a.stopAuto
	done := a.autoDone
	close(stop)
	a.autoMu.Unlock()
	<-done
}

func (a *App) Shutdown() {
	a.StopAuto()
}

func (a *App) markAutoStopped() {
	a.autoMu.Lock()
	a.autoRun = false
	a.autoMu.Unlock()
}

func (a *App) waitAutoDone() {
	select {
	case <-a.autoDone:
		return
	default:
	}
	<-a.autoDone
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
	a.mu.Lock()
	enabled := a.cfg.Auto.Enabled
	initialMS := a.cfg.Auto.InitialDelayMS
	intervalMS := a.cfg.Auto.IntervalMS
	a.mu.Unlock()
	if !enabled {
		a.appendLog(LogEvent{Type: "auto", Status: "disabled"})
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
	markets := a.cfg.Auto.Markets
	if len(markets) == 0 {
		markets = []string{"auction", "cera"}
	}
	for _, market := range markets {
		market = strings.ToLower(strings.TrimSpace(market))
		if market == "" {
			continue
		}
		if a.cfg.Collector.Enabled {
			a.appendLog(LogEvent{Type: "auto_collect", Market: market, Status: "start"})
			job, err := a.CollectOnce(CollectRequest{
				Market:          market,
				Execute:         true,
				MaxActions:      a.cfg.Auto.MaxActions,
				MaxConcurrent:   a.cfg.Auto.MaxConcurrent,
				ContinueOnError: a.cfg.Auto.ContinueOnError,
			})
			status := job.Status
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			a.appendLog(LogEvent{Type: "auto_collect", JobID: job.ID, Market: market, Status: status, Message: msg})
		}
		a.appendLog(LogEvent{Type: "auto_run", Market: market, Status: "start"})
		job, err := a.RestockOnce(RestockRequest{
			Market:          market,
			Execute:         true,
			MaxActions:      a.cfg.Auto.MaxActions,
			MaxConcurrent:   a.cfg.Auto.MaxConcurrent,
			ContinueOnError: a.cfg.Auto.ContinueOnError,
		})
		status := job.Status
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		a.appendLog(LogEvent{Type: "auto_run", JobID: job.ID, Market: market, Status: status, Message: msg})
	}
}
