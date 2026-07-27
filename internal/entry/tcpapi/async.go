package tcpapi

import (
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
	op := manager.BeginOperation(name, scope)
	go func() {
		defer asyncActions.Delete(name)
		summary, err := fn()
		manager.CompleteOperation(op.ID, summary, err)
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued", "operation": op}})
}

func queueExclusiveAction(name string, fn func()) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	go func() {
		defer asyncActions.Delete(name)
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
