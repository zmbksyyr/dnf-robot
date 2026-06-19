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
		return a.runtime.Move(uid)
	case robotActorShoutLocal:
		return a.runtime.Shout(uid, false)
	case robotActorShoutWorld:
		return a.runtime.Shout(uid, true)
	case robotActorStore:
		a.setOnlineDesired(true)
		res := a.runtime.Store(uid)
		if res.OK {
			rc := a.runtime.Config()
			a.storeUntil = time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second)
		}
		return res
	}
	return RobotActionResult{UID: uid, OK: false, State: "unknown_command"}
}
