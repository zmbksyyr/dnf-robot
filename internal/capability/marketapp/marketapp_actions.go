package marketapp

import (
	"fmt"
	"sync"
	"time"

	"robot/internal/foundation/lockhub"
)

type actionTask struct {
	index  int
	action Action
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
				firstErr = fmt.Errorf("action rejected reason=%s", actionLogReason(entry, nil))
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
					if actionRequiresAuctionID(task.action) && entry.AuctionID == 0 {
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
