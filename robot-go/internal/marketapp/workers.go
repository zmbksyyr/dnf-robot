package marketapp

import (
	"fmt"
	"sync"
	"time"
)

type actionTask struct {
	index  int
	action Action
}

func (a *App) executeActions(jobID string, actions []Action, maxConcurrent int, continueOnError bool, job *JobSummary) (int, error) {
	if len(actions) == 0 {
		return 0, nil
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
	var mu sync.Mutex
	failed := 0
	var firstErr error

	record := func(entry ActionEntry, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		} else if !entry.OK {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("action rejected reason=%v", byteValue(entry.Reason))
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
			executor := a.newActionExecutor()
			defer executor.close()
			for task := range tasks {
				select {
				case <-stop:
					return
				default:
				}
				entry := ActionEntry{Index: task.index, Action: task.action}
				res, err := executor.execute(task.action)
				if err != nil {
					entry.Error = err.Error()
					a.appendLog(LogEvent{Type: "action", JobID: jobID, Market: task.action.Market, ItemID: task.action.ItemID, OK: boolPtr(false), Message: err.Error()})
					record(entry, err)
				} else {
					entry.OK = res.ResultOK == nil || *res.ResultOK
					entry.AuctionID = res.AuctionID
					entry.Reason = res.ResultReason
					entry.Result = res
					a.appendLog(LogEvent{Type: "action", JobID: jobID, Market: task.action.Market, ItemID: task.action.ItemID, AuctionID: res.AuctionID, OK: &entry.OK, Reason: byteValue(entry.Reason)})
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
	return failed, firstErr
}
