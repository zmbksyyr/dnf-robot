package marketapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"robot/internal/foundation/lockhub"
)

type actionTask struct {
	index  int
	action Action
}

func (a *App) executeActions(ctx context.Context, jobID string, actions []Action, maxConcurrent int, continueOnError bool, job *JobSummary) (int, []ActionEntry, error) {
	if len(actions) == 0 {
		return 0, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg := a.configSnapshot()
	workers := maxConcurrent
	if workers <= 0 {
		workers = cfg.Restock.MaxConcurrent
	}
	if workers <= 0 {
		workers = 32
	}
	if workers > len(actions) {
		workers = len(actions)
	}
	delay := time.Duration(cfg.Restock.PerItemDelayMS) * time.Millisecond
	resultLimit := cfg.Restock.MaxResultActions
	if resultLimit <= 0 {
		resultLimit = 200
	}

	tasks := make(chan actionTask)
	var wg sync.WaitGroup
	var mu lockhub.Locker
	failed := 0
	entries := make([]ActionEntry, 0, len(actions))
	actionLog := newActionLogAccumulator()
	var firstErr error

	record := func(entry ActionEntry, err error) {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			a.applyAuctionActionFeedback(entry, err)
		}
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
				firstErr = fmt.Errorf("action rejected reason=%s", actionLogReason(entry, nil))
			}
		}
		if len(job.Actions) < resultLimit {
			job.Actions = append(job.Actions, entry)
		}
		if !continueOnError && firstErr != nil {
			cancel()
		}
	}

	executors := make([]ActionExecutor, 0, workers)
	for i := 0; i < workers; i++ {
		executor, err := newActionExecutorSafely(a.executors, cfg)
		if err != nil {
			for _, created := range executors {
				closeActionExecutorSafely(created)
			}
			return len(actions), nil, err
		}
		executors = append(executors, executor)
	}
	for _, executor := range executors {
		executor := executor
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer closeActionExecutorSafely(executor)
			defer func() {
				if rec := recover(); rec != nil {
					mu.Lock()
					failed++
					if firstErr == nil {
						firstErr = fmt.Errorf("action worker panic: %v", rec)
					}
					mu.Unlock()
					cancel()
				}
			}()
			var delayTimer *time.Timer
			if delay > 0 {
				delayTimer = time.NewTimer(delay)
				if !delayTimer.Stop() {
					<-delayTimer.C
				}
				defer delayTimer.Stop()
			}
			for task := range tasks {
				select {
				case <-workCtx.Done():
					return
				default:
				}
				entry := ActionEntry{Index: task.index, Action: task.action}
				res, err := executeActionSafely(executor, workCtx, task.action)
				if err != nil {
					entry.Error = err.Error()
					record(entry, err)
				} else {
					entry.OK = res.ResultOK != nil && *res.ResultOK
					entry.AuctionID = res.AuctionID
					entry.Reason = res.ResultReason
					entry.Result = res.Raw
					record(entry, nil)
				}
				if delayTimer != nil {
					delayTimer.Reset(delay)
					select {
					case <-delayTimer.C:
					case <-workCtx.Done():
						return
					}
				}
			}
		}()
	}

sendLoop:
	for i, action := range actions {
		select {
		case <-workCtx.Done():
			break sendLoop
		case tasks <- actionTask{index: i, action: action}:
		}
		select {
		case <-workCtx.Done():
			break sendLoop
		default:
		}
	}
	close(tasks)
	wg.Wait()
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	summary := actionLog.summary()
	a.appendLog(LogEvent{Type: "action_summary", JobID: jobID, ActionSummary: &summary})
	return failed, entries, firstErr
}

func newActionExecutorSafely(factory ActionExecutorFactory, cfg Config) (executor ActionExecutor, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("create action executor panic: %v", rec)
		}
	}()
	if factory == nil {
		return nil, ErrExecutorUnavailable
	}
	executor = factory.NewActionExecutor(cfg)
	if executor == nil {
		return nil, ErrExecutorUnavailable
	}
	return executor, nil
}

func executeActionSafely(executor ActionExecutor, ctx context.Context, action Action) (result ActionExecutionResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("execute action panic: %v", rec)
		}
	}()
	return executor.Execute(ctx, action)
}

func closeActionExecutorSafely(executor ActionExecutor) {
	if executor == nil {
		return
	}
	defer func() { _ = recover() }()
	executor.Close()
}
