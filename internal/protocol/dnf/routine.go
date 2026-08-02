package dnf

import (
	"runtime/debug"

	foundationlog "robot/internal/foundation/log"
)

// startRobotRoutine keeps optional protocol work from turning a malformed
// packet or an unexpected database/parser failure into a process-wide panic.
// Individual routines remain responsible for resetting their own state flags.
func startRobotRoutine(name string, uid uint32, fn func()) {
	if fn == nil {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				foundationlog.Robotf("[DNF_ASYNC_PANIC] name=%s uid=%d err=%v\n%s", name, uid, rec, debug.Stack())
			}
		}()
		fn()
	}()
}
