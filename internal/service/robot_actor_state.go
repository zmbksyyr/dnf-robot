package service

import (
	"time"
)

func (a *robotActor) releaseCurrentUID() int {
	uid := a.uidValue()
	if uid <= 0 {
		a.setState(robotActorIdle)
		return 0
	}
	a.setState(robotActorReleasing)
	if st, ok := a.runtime.Status(uid); ok && (st.RobotType == 2 || st.RobotType == 3 || st.StoreDisplayAck) {
		a.runtime.manager.finishStoreState(uid, st.CID, "release")
	}
	a.runtime.Logout(uid)
	a.resetForUID(0)
	return uid
}

func (a *robotActor) resetForUID(uid int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uid = uid
	a.clearAutoScheduleLocked()
	a.lastOnlineTry = time.Time{}
	a.firstFailureAt = time.Time{}
	a.failures = 0
	a.busy = false
	a.busyKind = ""
	a.releaseRequested = false
	a.onlineDesired = uid > 0
	if uid > 0 {
		a.state = robotActorAssigned
	} else {
		a.state = robotActorIdle
	}
}

func (a *robotActor) clearAutoSchedule() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clearAutoScheduleLocked()
}

func (a *robotActor) clearAutoScheduleLocked() {
	a.nextMove = time.Time{}
	a.nextLocalShout = time.Time{}
	a.nextWorldShout = time.Time{}
	a.nextStore = time.Time{}
	a.storeUntil = time.Time{}
}

func (a *robotActor) setReleaseRequested(v bool) {
	a.mu.Lock()
	a.releaseRequested = v
	a.mu.Unlock()
}

func (a *robotActor) releaseRequestedValue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.releaseRequested
}

func (a *robotActor) setOnlineDesired(v bool) {
	a.mu.Lock()
	a.onlineDesired = v
	a.mu.Unlock()
}

func (a *robotActor) onlineDesiredValue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.onlineDesired
}

func (a *robotActor) markOnlineHealthy() {
	a.mu.Lock()
	a.failures = 0
	a.firstFailureAt = time.Time{}
	a.lastOnlineTry = time.Time{}
	a.mu.Unlock()
}

func (a *robotActor) clearOnlineAttempt() {
	a.mu.Lock()
	a.lastOnlineTry = time.Time{}
	a.mu.Unlock()
}

func (a *robotActor) markOnlinePending(now time.Time) {
	a.mu.Lock()
	if a.firstFailureAt.IsZero() {
		a.firstFailureAt = now
	}
	a.mu.Unlock()
}

func (a *robotActor) onlineConfirmPending(uid int, now time.Time, rc robotRuntimeConfig) bool {
	lastOnlineTry := a.lastOnlineTryValue()
	if lastOnlineTry.IsZero() {
		return false
	}
	timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if now.Sub(lastOnlineTry) >= timeout {
		return false
	}
	st, ok := a.runtime.Status(uid)
	if !ok {
		return true
	}
	return st.DisconnectReason == 0 && (st.StateName == "init" || st.StateName == "login")
}

func (a *robotActor) lastOnlineTryValue() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastOnlineTry
}

func (a *robotActor) recordFailure(now time.Time) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	if a.firstFailureAt.IsZero() {
		a.firstFailureAt = now
	}
	return a.failures
}

func (a *robotActor) runBusy(kind string, fn func()) {
	a.setBusy(true, kind)
	defer a.setBusy(false, "")
	fn()
}

func (a *robotActor) setBusy(v bool, kind string) {
	a.mu.Lock()
	a.busy = v
	a.busyKind = kind
	if v {
		a.state = robotActorBusy
	} else if a.uid > 0 {
		if a.onlineDesired {
			a.state = robotActorRunning
		} else {
			a.state = robotActorOffline
		}
	}
	a.mu.Unlock()
}

func (a *robotActor) busyValue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy
}

func (a *robotActor) setState(state robotActorState) {
	a.mu.Lock()
	a.state = state
	a.mu.Unlock()
}

func (a *robotActor) snapshot() robotActorSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return robotActorSnapshot{
		SlotID:         a.slotID,
		UID:            a.uid,
		Mode:           a.mode,
		State:          a.state,
		Busy:           a.busy,
		BusyKind:       a.busyKind,
		OnlineDesired:  a.onlineDesired,
		LastOnlineTry:  a.lastOnlineTry,
		FirstFailureAt: a.firstFailureAt,
		Failures:       a.failures,
	}
}

func (a *robotActor) uidValue() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.uid
}

func (a *robotActor) modeValue() robotActorMode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

func (a *robotActor) stateValue() robotActorState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (s robotActorSnapshot) empty() bool {
	return s.UID <= 0
}

func (s robotActorSnapshot) schedulerPending() bool {
	if s.empty() {
		return true
	}
	switch s.State {
	case robotActorIdle, robotActorOffline, robotActorAssigned, robotActorOnline, robotActorReleasing:
		return true
	default:
		return false
	}
}
