package service

import (
	"time"
)

func (a *robotActor) logoutCurrentUID() RobotActionResult {
	uid := a.uidValue()
	if uid <= 0 {
		a.setState(robotActorIdle)
		return RobotActionResult{OK: true, State: "idle"}
	}
	a.setOnlineDesired(false)
	a.clearAutoSchedule()
	a.setBusy(true, "logout")
	defer a.setBusy(false, "")
	res := a.runtime.Logout(uid)
	if res.UID == 0 {
		res.UID = uid
	}
	if res.OK {
		res.State = "attached_offline"
	}
	a.setState(robotActorOffline)
	return res
}

func (a *robotActor) handleCommand(cmd robotActorCommand) RobotActionResult {
	uid := a.uidValue()
	if uid <= 0 {
		return RobotActionResult{OK: false, State: "idle", Message: "actor has no uid"}
	}
	switch cmd {
	case robotActorCmdOnline:
		a.setOnlineDesired(true)
		a.setState(robotActorAssigned)
		return RobotActionResult{UID: uid, OK: true, State: "accepted"}
	case robotActorCmdLogout:
		return a.logoutCurrentUID()
	}
	a.setBusy(true, "command")
	defer a.setBusy(false, "")
	switch cmd {
	case robotActorMove:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Move(uid)
	case robotActorShoutLocal:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Shout(uid, false)
	case robotActorShoutWorld:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Shout(uid, true)
	case robotActorStore:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		res := a.runtime.Store(uid)
		if res.OK {
			rc := a.runtime.Config()
			a.storeUntil = time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second)
		}
		return res
	}
	return RobotActionResult{UID: uid, OK: false, State: "unknown_command"}
}

func (a *robotActor) ensureOnlineForCommand() (RobotActionResult, bool) {
	uid := a.uidValue()
	if uid <= 0 {
		return RobotActionResult{OK: false, State: "idle", Message: "actor has no uid"}, false
	}
	if a.runtime.IsActive(uid) {
		return RobotActionResult{UID: uid, OK: true, State: "running"}, true
	}
	a.setOnlineDesired(true)
	a.setState(robotActorAssigned)
	rc := a.runtime.Config()
	timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.releaseRequestedValue() {
			return RobotActionResult{UID: uid, OK: false, State: "cancelled"}, false
		}
		if a.runtime.IsActive(uid) {
			return RobotActionResult{UID: uid, OK: true, State: "running"}, true
		}
		a.ensureOnline(time.Now())
		time.Sleep(500 * time.Millisecond)
	}
	return RobotActionResult{UID: uid, OK: false, State: "online_timeout", Message: "not confirmed running"}, false
}
