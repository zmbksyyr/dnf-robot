package actor

import (
	"time"

	robotcap "robot/internal/capability/robot"
	foundationlog "robot/internal/foundation/log"
)

type request struct {
	cmd  Command
	done chan robotcap.ActionResult
}

type control struct {
	kind controlKind
	uid  int
	done chan controlResult
}

type controlResult struct {
	uid int
	ok  bool
}

func (a *Actor) start() {
	go a.loop()
}

func (a *Actor) assignAndWait(uid int, timeout time.Duration) bool {
	if uid <= 0 {
		return false
	}
	res := a.controlAndWait(control{kind: controlAssign, uid: uid}, timeout)
	return res.ok
}

func (a *Actor) releaseAndWait(timeout time.Duration) int {
	a.setReleaseRequested(true)
	res := a.controlAndWait(control{kind: controlRelease}, timeout)
	return res.uid
}

func (a *Actor) controlAndWait(ctrl control, timeout time.Duration) controlResult {
	ctrl.done = make(chan controlResult, 1)
	select {
	case a.ctrls <- ctrl:
	case <-a.done:
		return controlResult{}
	default:
		return controlResult{}
	}
	if timeout <= 0 {
		select {
		case res := <-ctrl.done:
			return res
		case <-a.done:
			return controlResult{}
		}
	}
	select {
	case res := <-ctrl.done:
		return res
	case <-a.done:
		return controlResult{}
	case <-time.After(timeout):
		return controlResult{}
	}
}

func (a *Actor) stopAndWait(timeout time.Duration) {
	a.setReleaseRequested(true)
	a.once.Do(func() { close(a.stop) })
	if timeout <= 0 {
		<-a.done
		return
	}
	select {
	case <-a.done:
	case <-time.After(timeout):
		foundationlog.Robotf("[Actor] stop_timeout slot=%d uid=%d timeout=%s\n", a.slotIDValue(), a.uidValue(), timeout)
	}
}

func (a *Actor) enqueue(cmd Command, timeout time.Duration) (robotcap.ActionResult, bool) {
	req := request{cmd: cmd, done: make(chan robotcap.ActionResult, 1)}
	select {
	case a.cmds <- req:
	default:
		return robotcap.ActionResult{UID: a.uidValue(), OK: false, State: robotcap.ActionStateQueueFailed}, false
	}
	if timeout <= 0 {
		return robotcap.ActionResult{UID: a.uidValue(), OK: true, State: robotcap.ActionStateAccepted}, true
	}
	select {
	case res := <-req.done:
		return res, true
	case <-time.After(timeout):
		return robotcap.ActionResult{UID: a.uidValue(), OK: false, State: robotcap.ActionStateTimeout, Message: "manual action timeout"}, false
	}
}

func (a *Actor) loop() {
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

func (a *Actor) handleControl(ctrl control) controlResult {
	switch ctrl.kind {
	case controlAssign:
		if ctrl.uid <= 0 {
			return controlResult{}
		}
		if old := a.uidValue(); old > 0 && old != ctrl.uid {
			a.releaseCurrentUID()
		}
		a.resetForUID(ctrl.uid)
		a.setReleaseRequested(false)
		return controlResult{uid: ctrl.uid, ok: true}
	case controlRelease:
		old := a.releaseCurrentUID()
		return controlResult{uid: old, ok: true}
	}
	return controlResult{}
}

func (a *Actor) releaseCurrentUID() int {
	uid := a.uidValue()
	if uid <= 0 {
		a.setState(StateIdle)
		return 0
	}
	a.setState(StateReleasing)
	cid := 0
	if st, ok := a.runtime.Status(uid); ok {
		cid = st.CID
	}
	a.runtime.FinishStoreState(uid, cid, "release")
	a.runtime.Logout(uid)
	a.resetForUID(0)
	return uid
}

func (a *Actor) AssignAndWait(uid int, timeout time.Duration) bool {
	return a.assignAndWait(uid, timeout)
}

func (a *Actor) ReleaseAndWait(timeout time.Duration) int {
	return a.releaseAndWait(timeout)
}

func (a *Actor) StopAndWait(timeout time.Duration) {
	a.stopAndWait(timeout)
}

func (a *Actor) Enqueue(cmd Command, timeout time.Duration) (robotcap.ActionResult, bool) {
	return a.enqueue(cmd, timeout)
}

func (a *Actor) Start() {
	a.start()
}
