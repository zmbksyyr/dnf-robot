package marketapp

import (
	"fmt"
	"time"
)

func busyMarketJob(kind string) JobSummary {
	now := time.Now()
	return JobSummary{
		ID:        fmt.Sprintf("%s-busy-%d", kind, now.UnixNano()),
		Kind:      kind,
		Status:    MarketJobStatusBusy,
		Error:     "market job already running",
		StartedAt: now,
		EndedAt:   now,
	}
}

func (a *App) RestockOnce(req RestockRequest) (JobSummary, error) {
	if !a.jobMu.TryLock() {
		job := busyMarketJob("restock")
		return job, fmt.Errorf(job.Error)
	}
	defer a.jobMu.Unlock()
	if req.MaxActions <= 0 {
		req.MaxActions = a.cfg.Restock.MaxActions
	}
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("restock-%d", start.UnixNano()),
		Kind:      "restock",
		Status:    MarketJobStatusRunning,
		StartedAt: start,
	}
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.Plan(req)
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
		maxActions = a.cfg.Restock.MaxActions
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
	a.reconcileCeraRejects(entries)
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
		a.applyRestockDBConfirmation(&job, actions)
	}
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
	return job, nil
}

func (a *App) applyRestockDBConfirmation(job *JobSummary, actions []Action) {
	if !needsAuctionDBConfirmation(actions) {
		job.Status = MarketJobStatusSuccess
		job.Error = ""
		return
	}
	confirmed, err := a.auctionDBConfirmed(actions)
	if err != nil {
		job.Status = MarketJobStatusPartialFailed
		job.Error = fmt.Sprintf("auction db confirmation failed: %v", err)
		return
	}
	if !confirmed {
		job.Status = MarketJobStatusPendingDB
		job.Error = "auction register acked; waiting for DB fact confirmation"
		return
	}
	job.Status = MarketJobStatusSuccess
	job.Error = ""
}

func needsAuctionDBConfirmation(actions []Action) bool {
	for _, action := range actions {
		if action.Market == marketNameAuction && action.Operation != "collect" {
			return true
		}
	}
	return false
}

func (a *App) auctionDBConfirmed(actions []Action) (bool, error) {
	watch := map[uint32]bool{}
	for _, action := range actions {
		if action.Market == marketNameAuction && action.Operation != "collect" && action.ItemID > 0 {
			watch[action.ItemID] = true
		}
	}
	if len(watch) == 0 {
		return true, nil
	}
	have, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		return false, err
	}
	for itemID := range watch {
		if have[itemID] > 0 {
			return true, nil
		}
	}
	return false, nil
}
