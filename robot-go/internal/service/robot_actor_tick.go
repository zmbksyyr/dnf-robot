package service

import (
	"time"
)

func (a *robotActor) tick(now time.Time) {
	if a.releaseRequestedValue() || a.stateValue() == robotActorReleasing {
		return
	}
	uid := a.uidValue()
	if uid <= 0 {
		return
	}
	if !a.onlineDesiredValue() {
		if a.stateValue() != robotActorOffline && !a.busyValue() {
			a.setState(robotActorOffline)
		}
		return
	}
	rc := a.runtime.Config()
	if !a.runtime.IsActive(uid) {
		a.ensureOnline(now)
		return
	}
	st, ok := a.runtime.Status(uid)
	if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
		return
	}
	if a.stateValue() != robotActorRunning {
		a.runtime.manager.addAutoOnline(1, 0)
	}
	a.markOnlineHealthy()
	a.setState(robotActorRunning)
	isStore := st.RobotType == 2 || st.RobotType == 3
	if isStore && !a.storeUntil.IsZero() && now.After(a.storeUntil) {
		a.runBusy("store_expire", func() {
			a.runtime.ExpireStore(uid)
		})
		a.storeUntil = time.Time{}
		return
	}
	if a.modeValue() != robotActorAuto {
		return
	}
	if !a.runtime.manager.autoActionsEnabled(rc) {
		return
	}
	if a.randomizedDue(&a.nextWorldShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec) {
		a.runBusy("shout_world", func() {
			a.runtime.AutoShout(uid, true, a.randomShoutMessage())
		})
	}
	if a.randomizedDue(&a.nextLocalShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec) {
		a.runBusy("shout_local", func() {
			a.runtime.AutoShout(uid, false, a.randomShoutMessage())
		})
	}
	if !isStore && a.randomizedDue(&a.nextStore, now, rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec) {
		if rc.AutoStoreProbabilityPercent > 0 && a.randIntn(100) < rc.AutoStoreProbabilityPercent {
			var res RobotActionResult
			a.runBusy("store", func() {
				defer a.clearOnlineAttempt()
				res = a.runtime.AutoStore(uid, a.releaseRequestedValue)
			})
			if res.OK {
				a.storeUntil = time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second)
			} else if res.State == "store_busy" {
				a.markOnlineHealthy()
				a.nextStore = time.Now().Add(time.Duration(rc.AutoStoreFailCooldownSec) * time.Second)
			} else if res.State == "store_failed" {
				a.markOnlineHealthy()
				a.nextStore = time.Now().Add(time.Duration(rc.AutoStoreFailCooldownSec) * time.Second)
			} else if res.State != "cancelled" {
				a.recordFailure(time.Now())
			}
			return
		}
	}
	if !isStore && a.randomizedDue(&a.nextMove, now, rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec) {
		a.runBusy("move", func() {
			a.runtime.AutoMove(uid)
		})
	}
}

func (a *robotActor) randomShoutMessage() string {
	tpl := a.runtime.manager.loadShoutTemplates()
	if len(tpl.Messages) == 0 {
		return ""
	}
	return safeRobotShoutMessage(tpl.Messages[a.randIntn(len(tpl.Messages))])
}

func (a *robotActor) ensureOnline(now time.Time) {
	uid := a.uidValue()
	if uid <= 0 {
		return
	}
	if a.runtime.IsActive(uid) {
		a.markOnlineHealthy()
		a.setState(robotActorRunning)
		a.runtime.manager.addAutoOnline(1, 0)
		return
	}
	rc := a.runtime.Config()
	if a.onlineAttemptTimedOut(uid, now, rc) {
		failures := a.recordFailure(now)
		a.runtime.manager.addAutoOnline(0, 1)
		robotLogf("[RobotActor] online_confirm_timeout slot=%d uid=%d failures=%d\n", a.slotID, uid, failures)
		a.clearOnlineAttempt()
		return
	}
	if a.onlineConfirmPending(uid, now, rc) {
		a.markOnlinePending(now)
		return
	}
	if now.Sub(a.lastOnlineTryValue()) < time.Duration(rc.ReconnectDelayMS)*time.Millisecond {
		return
	}
	a.lastOnlineTry = now
	a.setState(robotActorOnline)
	res := a.runtime.OnlineNoConfirm(uid)
	if a.releaseRequestedValue() {
		return
	}
	if res.OK || res.State == "running" {
		a.markOnlineHealthy()
		a.runtime.manager.addAutoOnline(1, 0)
		return
	}
	if res.State == "accepted" || res.State == "init" || res.State == "login" {
		a.markOnlinePending(now)
		return
	}
	failures := a.recordFailure(now)
	a.runtime.manager.addAutoOnline(0, 1)
	robotLogf("[RobotActor] online_failed slot=%d uid=%d failures=%d state=%s msg=%s\n", a.slotID, uid, failures, res.State, res.Message)
}
