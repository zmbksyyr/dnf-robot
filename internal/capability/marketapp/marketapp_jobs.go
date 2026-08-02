package marketapp

import (
	"context"
	"errors"
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
	return a.restockOnce(a.lifecycleContext(), req)
}

func (a *App) restockOnce(ctx context.Context, req RestockRequest) (JobSummary, error) {
	if err := ctx.Err(); err != nil {
		return cancelledMarketJob("restock", err), err
	}
	if !a.jobMu.TryLock() {
		job := busyMarketJob("restock")
		return job, fmt.Errorf(job.Error)
	}
	defer a.jobMu.Unlock()
	cfg := a.configSnapshot()
	if req.MaxActions <= 0 {
		req.MaxActions = cfg.Restock.MaxActions
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
	if err := ctx.Err(); err != nil {
		return a.finishCancelledJob(job, err)
	}
	job.Plan = &plan.Summary
	maxActions := req.MaxActions
	if maxActions <= 0 {
		maxActions = cfg.Restock.MaxActions
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
	failedActions, entries, firstErr := a.executeActions(ctx, job.ID, actions, req.MaxConcurrent, req.ContinueOnError, &job)
	a.reconcileCeraRejects(entries)
	a.reconcileCeraLanding(ctx, entries)
	if err := ctx.Err(); err != nil || errors.Is(firstErr, context.Canceled) || errors.Is(firstErr, context.DeadlineExceeded) {
		if err == nil {
			err = firstErr
		}
		return a.finishCancelledJob(job, err)
	}
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
		a.applyRestockDBConfirmation(ctx, &job, actions)
	}
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
	return job, nil
}

func (a *App) applyRestockDBConfirmation(ctx context.Context, job *JobSummary, actions []Action) {
	if err := ctx.Err(); err != nil {
		job.Status = MarketJobStatusCancelled
		job.Error = err.Error()
		return
	}
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

func cancelledMarketJob(kind string, err error) JobSummary {
	now := time.Now()
	return JobSummary{
		ID:        fmt.Sprintf("%s-cancelled-%d", kind, now.UnixNano()),
		Kind:      kind,
		Status:    MarketJobStatusCancelled,
		Error:     err.Error(),
		StartedAt: now,
		EndedAt:   now,
	}
}

func (a *App) finishCancelledJob(job JobSummary, err error) (JobSummary, error) {
	job.Status = MarketJobStatusCancelled
	job.Error = err.Error()
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error, Summary: job.Plan})
	return job, err
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
	cfg := a.configSnapshot()
	have, err := a.repository.LoadMarketStock(cfg.AuctionDB, cfg.SystemOwner.IDBase, map[uint32]int{})
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
