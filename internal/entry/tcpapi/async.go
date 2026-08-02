package tcpapi

import (
	"fmt"
	"sync"

	robotcap "robot/internal/capability/robot"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/scheduler"
)

var asyncActions sync.Map

func queueRobotAction(manager *scheduler.RobotManager, name, scope string, fn func() (string, error)) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	finish, ok := manager.BeginBackgroundWork()
	if !ok {
		asyncActions.Delete(name)
		return wrapResult(map[string]interface{}{"ok": false, "error": "robot manager is shutting down"})
	}
	op := manager.BeginOperation(name, scope)
	go func() {
		defer asyncActions.Delete(name)
		defer finish()
		defer func() {
			if rec := recover(); rec != nil {
				err := fmt.Errorf("panic: %v", rec)
				manager.CompleteOperation(op.ID, "", err)
				logRobotActionf("[WebAction] %s panic err=%v\n", name, rec)
			}
		}()
		summary, err := fn()
		manager.CompleteOperation(op.ID, summary, err)
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued", "operation": op}})
}

func queueExclusiveAction(manager *scheduler.RobotManager, name string, fn func()) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	finish, ok := manager.BeginBackgroundWork()
	if !ok {
		asyncActions.Delete(name)
		return wrapResult(map[string]interface{}{"ok": false, "error": "robot manager is shutting down"})
	}
	go func() {
		defer asyncActions.Delete(name)
		defer finish()
		defer func() {
			if rec := recover(); rec != nil {
				logRobotActionf("[WebAction] %s panic err=%v\n", name, rec)
			}
		}()
		fn()
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued"}})
}

func logRobotCommandResult(name string, res robotcap.CommandResult, err error) {
	if err != nil {
		logRobotActionf("[WebAction] %s failed err=%v\n", name, err)
		return
	}
	logRobotActionf("[WebAction] %s done requested=%d accepted=%d confirmed=%d failed=%d\n",
		name, res.Requested, res.Accepted, res.Confirmed, res.Failed)
}

func logRobotActionf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}
