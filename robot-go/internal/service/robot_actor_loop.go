package service

import (
	"time"
)

func (a *robotActor) assignAndWait(uid int, timeout time.Duration) bool {
	if uid <= 0 {
		return false
	}
	res := a.controlAndWait(robotActorControl{kind: robotActorAssign, uid: uid}, timeout)
	return res.ok
}

func (a *robotActor) releaseAndWait(timeout time.Duration) int {
	a.setReleaseRequested(true)
	res := a.controlAndWait(robotActorControl{kind: robotActorRelease}, timeout)
	return res.uid
}

func (a *robotActor) controlAndWait(ctrl robotActorControl, timeout time.Duration) robotActorControlResult {
	ctrl.done = make(chan robotActorControlResult, 1)
	select {
	case a.ctrls <- ctrl:
	case <-a.done:
		return robotActorControlResult{}
	default:
		return robotActorControlResult{}
	}
	if timeout <= 0 {
		select {
		case res := <-ctrl.done:
			return res
		case <-a.done:
			return robotActorControlResult{}
		}
	}
	select {
	case res := <-ctrl.done:
		return res
	case <-a.done:
		return robotActorControlResult{}
	case <-time.After(timeout):
		return robotActorControlResult{}
	}
}

func (a *robotActor) stopAndWait(timeout time.Duration) {
	a.setReleaseRequested(true)
	a.once.Do(func() { close(a.stop) })
	if timeout <= 0 {
		<-a.done
		return
	}
	select {
	case <-a.done:
	case <-time.After(timeout):
		robotLogf("[RobotActor] stop_timeout slot=%d uid=%d timeout=%s\n", a.slotID, a.uidValue(), timeout)
	}
}

func (a *robotActor) enqueue(cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool) {
	req := robotActorRequest{cmd: cmd, done: make(chan RobotActionResult, 1)}
	select {
	case a.cmds <- req:
	default:
		return RobotActionResult{UID: a.uidValue(), OK: false, State: "queue_failed"}, false
	}
	if timeout <= 0 {
		return RobotActionResult{UID: a.uidValue(), OK: true, State: "accepted"}, true
	}
	select {
	case res := <-req.done:
		return res, true
	case <-time.After(timeout):
		return RobotActionResult{UID: a.uidValue(), OK: false, State: "timeout", Message: "manual action timeout"}, false
	}
}

func (a *robotActor) loop() {
	defer close(a.done)
	rc := a.runtime.Config()
	ticker := time.NewTicker(time.Duration(rc.SystemActorPollMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			a.releaseCurrentUID()
			return
		case ctrl := <-a.ctrls:
			res := a.handleControl(ctrl)
			select {
			case ctrl.done <- res:
			default:
			}
		case req := <-a.cmds:
			res := a.handleCommand(req.cmd)
			select {
			case req.done <- res:
			default:
			}
		case now := <-ticker.C:
			a.tick(now)
		}
	}
}

func (a *robotActor) handleControl(ctrl robotActorControl) robotActorControlResult {
	switch ctrl.kind {
	case robotActorAssign:
		if ctrl.uid <= 0 {
			return robotActorControlResult{}
		}
		if old := a.uidValue(); old > 0 && old != ctrl.uid {
			a.releaseCurrentUID()
		}
		a.resetForUID(ctrl.uid)
		a.setReleaseRequested(false)
		return robotActorControlResult{uid: ctrl.uid, ok: true}
	case robotActorRelease:
		old := a.releaseCurrentUID()
		return robotActorControlResult{uid: old, ok: true}
	}
	return robotActorControlResult{}
}
