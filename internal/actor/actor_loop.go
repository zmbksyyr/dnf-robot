package actor

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	foundationlog "robot/internal/foundation/log"
	"time"
)

// ---- mailbox.go ----
type request struct {
	cmd  Command
	done chan robotcap.ActionResult
}

type control struct {
	kind ControlKind
	uid  int
	done chan ControlResult
}

type ControlResult struct {
	uid int
	ok  bool
}

func (a *Actor) status(now time.Time, rc robotconfig.RuntimeConfig) Status {
	s := a.snapshot()
	var lookup RuntimeStatusLookup
	if a.runtime != nil {
		lookup = a.runtime.Status
	}
	return EvaluateStatus(s, now, StatusConfig{
		BadFailures:            rc.SchedulerBadFailures,
		OnlineConfirmTimeoutMS: rc.OnlineConfirmTimeoutMS,
	}, lookup)
}

func (a *Actor) start() {
	go a.loop()
}

func (a *Actor) assignAndWait(uid int, timeout time.Duration) bool {
	if uid <= 0 {
		return false
	}
	res := a.controlAndWait(control{kind: ControlAssign, uid: uid}, timeout)
	return res.ok
}

func (a *Actor) releaseAndWait(timeout time.Duration) int {
	a.setReleaseRequested(true)
	res := a.controlAndWait(control{kind: ControlRelease}, timeout)
	return res.uid
}

func (a *Actor) controlAndWait(ctrl control, timeout time.Duration) ControlResult {
	ctrl.done = make(chan ControlResult, 1)
	select {
	case a.ctrls <- ctrl:
	case <-a.done:
		return ControlResult{}
	default:
		return ControlResult{}
	}
	if timeout <= 0 {
		select {
		case res := <-ctrl.done:
			return res
		case <-a.done:
			return ControlResult{}
		}
	}
	select {
	case res := <-ctrl.done:
		return res
	case <-a.done:
		return ControlResult{}
	case <-time.After(timeout):
		return ControlResult{}
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

func (a *Actor) handleControl(ctrl control) ControlResult {
	switch ctrl.kind {
	case ControlAssign:
		if ctrl.uid <= 0 {
			return ControlResult{}
		}
		if old := a.uidValue(); old > 0 && old != ctrl.uid {
			a.releaseCurrentUID()
		}
		a.resetForUID(ctrl.uid)
		a.setReleaseRequested(false)
		return ControlResult{uid: ctrl.uid, ok: true}
	case ControlRelease:
		old := a.releaseCurrentUID()
		return ControlResult{uid: old, ok: true}
	}
	return ControlResult{}
}

// ---- behavior.go ----
func (a *Actor) logoutCurrentUID() robotcap.ActionResult {
	uid := a.uidValue()
	if uid <= 0 {
		a.setState(StateIdle)
		return robotcap.ActionResult{OK: true, State: robotcap.ActionStateIdle}
	}
	cid := 0
	if st, ok := a.runtime.Status(uid); ok {
		cid = st.CID
	}
	a.runtime.FinishStoreState(uid, cid, "logout")
	a.setOnlineDesired(false)
	a.clearAutoSchedule()
	a.setBusy(true, "logout")
	defer a.setBusy(false, "")
	res := a.runtime.Logout(uid)
	if res.UID == 0 {
		res.UID = uid
	}
	if res.OK {
		res.State = robotcap.ActionStateAttachedOffline
	}
	a.setState(StateOffline)
	return res
}

func (a *Actor) handleCommand(cmd Command) robotcap.ActionResult {
	uid := a.uidValue()
	if uid <= 0 {
		return robotcap.ActionResult{OK: false, State: robotcap.ActionStateIdle, Message: "actor has no uid"}
	}
	switch cmd {
	case CommandOnline:
		a.setOnlineDesired(true)
		a.setState(StateAssigned)
		return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateAccepted}
	case CommandLogout:
		return a.logoutCurrentUID()
	}
	a.setBusy(true, "command")
	defer a.setBusy(false, "")
	switch cmd {
	case CommandMove:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Move(uid)
	case CommandShoutLocal:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Shout(uid, false)
	case CommandShoutWorld:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		return a.runtime.Shout(uid, true)
	case CommandStore:
		if res, ok := a.ensureOnlineForCommand(); !ok {
			return res
		}
		res := a.runtime.Store(uid)
		if res.OK {
			rc := a.runtime.Config()
			a.setStoreUntil(time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second))
		}
		return res
	}
	return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateUnknownCommand}
}

func (a *Actor) ensureOnlineForCommand() (robotcap.ActionResult, bool) {
	uid := a.uidValue()
	if uid <= 0 {
		return robotcap.ActionResult{OK: false, State: robotcap.ActionStateIdle, Message: "actor has no uid"}, false
	}
	if a.runtime.IsActive(uid) {
		return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateRunning}, true
	}
	a.setOnlineDesired(true)
	a.setState(StateAssigned)
	rc := a.runtime.Config()
	timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.releaseRequestedValue() {
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateCancelled}, false
		}
		if a.runtime.IsActive(uid) {
			return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateRunning}, true
		}
		a.ensureOnline(time.Now())
		time.Sleep(500 * time.Millisecond)
	}
	return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOnlineTimeout, Message: "not confirmed running"}, false
}

func (a *Actor) tick(now time.Time) {
	if a.releaseRequestedValue() || a.stateValue() == StateReleasing {
		return
	}
	uid := a.uidValue()
	if uid <= 0 {
		return
	}
	if !a.onlineDesiredValue() {
		if a.stateValue() != StateOffline && !a.busyValue() {
			a.setState(StateOffline)
		}
		return
	}
	rc := a.runtime.Config()
	if !a.runtime.IsActive(uid) {
		a.ensureOnline(now)
		return
	}
	st, ok := a.runtime.Status(uid)
	if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
		return
	}
	if a.stateValue() != StateRunning {
		a.runtime.AddAutoOnline(1, 0)
	}
	a.markOnlineHealthy()
	a.setState(StateRunning)
	isStore := st.RobotType == 2 || st.RobotType == 3
	if isStore && a.storeUntilMissing() {
		rc := a.runtime.Config()
		grace := time.Duration(rc.AutoStoreTickSec) * time.Second
		if grace <= 0 {
			grace = 10 * time.Second
		}
		a.setStoreUntil(now.Add(grace))
		foundationlog.Robotf("[Actor] store_expire_recovered slot=%d uid=%d grace=%s\n", a.slotIDValue(), uid, grace)
	}
	if isStore && a.storeExpired(now) {
		a.runBusy("store_expire", func() {
			a.runtime.ExpireStore(uid)
		})
		a.clearStoreUntil()
		return
	}
	if a.modeValue() != ModeAuto {
		return
	}
	if !a.runtime.AutoActionsEnabled(rc) {
		return
	}
	if a.nextWorldShoutDue(now, rc) {
		a.runBusy("shout_world", func() {
			a.runtime.AutoShout(uid, true, a.randomShoutMessage())
		})
	}
	if a.nextLocalShoutDue(now, rc) {
		a.runBusy("shout_local", func() {
			a.runtime.AutoShout(uid, false, a.randomShoutMessage())
		})
	}
	if !isStore && a.nextStoreDue(now, rc) {
		if rc.AutoStoreProbabilityPercent > 0 && a.randIntn(100) < rc.AutoStoreProbabilityPercent {
			var res robotcap.ActionResult
			a.runBusy("store", func() {
				defer a.clearOnlineAttempt()
				res = a.runtime.AutoStore(uid, a.releaseRequestedValue)
			})
			if res.OK {
				a.setStoreUntil(time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second))
			} else if res.State == robotcap.ActionStateStoreBusy {
				a.markOnlineHealthy()
				a.setNextStore(time.Now().Add(time.Duration(rc.AutoStoreFailCooldownSec) * time.Second))
			} else if res.State == robotcap.ActionStateStoreFailed {
				a.markOnlineHealthy()
				a.setNextStore(time.Now().Add(time.Duration(rc.AutoStoreFailCooldownSec) * time.Second))
			} else if res.State != robotcap.ActionStateCancelled {
				a.recordFailure(time.Now())
			}
			return
		}
	}
	if !isStore && a.nextMoveDue(now, rc) {
		a.runBusy("move", func() {
			a.runtime.AutoMove(uid)
		})
	}
}

func (a *Actor) randomShoutMessage() string {
	return a.runtime.RandomShoutMessage(a.randIntn)
}

func (a *Actor) ensureOnline(now time.Time) {
	uid := a.uidValue()
	if uid <= 0 {
		return
	}
	if a.runtime.IsActive(uid) {
		a.markOnlineHealthy()
		a.setState(StateRunning)
		a.runtime.AddAutoOnline(1, 0)
		return
	}
	rc := a.runtime.Config()
	if a.onlineAttemptTimedOut(uid, now, rc) {
		failures := a.recordFailure(now)
		a.runtime.AddAutoOnline(0, 1)
		foundationlog.Robotf("[Actor] online_confirm_timeout slot=%d uid=%d failures=%d\n", a.slotIDValue(), uid, failures)
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
	a.setLastOnlineTry(now)
	a.setState(StateOnline)
	res := a.runtime.OnlineNoConfirm(uid)
	if a.releaseRequestedValue() {
		return
	}
	if res.OK || res.State == robotcap.ActionStateRunning {
		a.markOnlineHealthy()
		a.runtime.AddAutoOnline(1, 0)
		return
	}
	if res.State == robotcap.ActionStateAccepted || res.State == robotcap.RuntimeStateInit || res.State == robotcap.RuntimeStateLogin {
		a.markOnlinePending(now)
		return
	}
	failures := a.recordFailure(now)
	a.runtime.AddAutoOnline(0, 1)
	foundationlog.Robotf("[Actor] online_failed slot=%d uid=%d failures=%d state=%s msg=%s\n", a.slotIDValue(), uid, failures, res.State, res.Message)
}

// ---- state.go ----
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

func (a *Actor) resetForUID(uid int) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
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
		a.state = StateAssigned
	} else {
		a.state = StateIdle
	}
}

func (a *Actor) clearAutoSchedule() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.clearAutoScheduleLocked()
}

func (a *Actor) clearAutoScheduleLocked() {
	a.nextMove = time.Time{}
	a.nextLocalShout = time.Time{}
	a.nextWorldShout = time.Time{}
	a.nextStore = time.Time{}
	a.storeUntil = time.Time{}
}

func (a *Actor) setReleaseRequested(v bool) {
	a.stateMu.Lock()
	a.releaseRequested = v
	a.stateMu.Unlock()
}

func (a *Actor) releaseRequestedValue() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.releaseRequested
}

func (a *Actor) setOnlineDesired(v bool) {
	a.stateMu.Lock()
	a.onlineDesired = v
	a.stateMu.Unlock()
}

func (a *Actor) onlineDesiredValue() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.onlineDesired
}

func (a *Actor) markOnlineHealthy() {
	a.stateMu.Lock()
	a.failures = 0
	a.firstFailureAt = time.Time{}
	a.lastOnlineTry = time.Time{}
	a.stateMu.Unlock()
}

func (a *Actor) clearOnlineAttempt() {
	a.stateMu.Lock()
	a.lastOnlineTry = time.Time{}
	a.stateMu.Unlock()
}

func (a *Actor) markOnlinePending(now time.Time) {
	a.stateMu.Lock()
	if a.firstFailureAt.IsZero() {
		a.firstFailureAt = now
	}
	a.stateMu.Unlock()
}

func (a *Actor) onlineConfirmPending(uid int, now time.Time, rc robotconfig.RuntimeConfig) bool {
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
	return st.DisconnectReason == 0 && (st.StateName == robotcap.RuntimeStateInit || st.StateName == robotcap.RuntimeStateLogin)
}

func (a *Actor) onlineAttemptTimedOut(uid int, now time.Time, rc robotconfig.RuntimeConfig) bool {
	lastOnlineTry := a.lastOnlineTryValue()
	if lastOnlineTry.IsZero() {
		return false
	}
	timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if now.Sub(lastOnlineTry) < timeout {
		return false
	}
	st, ok := a.runtime.Status(uid)
	if !ok {
		return true
	}
	return st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0
}

func (a *Actor) lastOnlineTryValue() time.Time {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.lastOnlineTry
}

func (a *Actor) setLastOnlineTry(t time.Time) {
	a.stateMu.Lock()
	a.lastOnlineTry = t
	a.stateMu.Unlock()
}

func (a *Actor) storeExpired(now time.Time) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return !a.storeUntil.IsZero() && now.After(a.storeUntil)
}

func (a *Actor) storeUntilMissing() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.storeUntil.IsZero()
}

func (a *Actor) clearStoreUntil() {
	a.stateMu.Lock()
	a.storeUntil = time.Time{}
	a.stateMu.Unlock()
}

func (a *Actor) setStoreUntil(t time.Time) {
	a.stateMu.Lock()
	a.storeUntil = t
	a.stateMu.Unlock()
}

func (a *Actor) setNextStore(t time.Time) {
	a.stateMu.Lock()
	a.nextStore = t
	a.stateMu.Unlock()
}

func (a *Actor) nextWorldShoutDue(now time.Time, rc robotconfig.RuntimeConfig) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.randomizedDue(&a.nextWorldShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
}

func (a *Actor) nextLocalShoutDue(now time.Time, rc robotconfig.RuntimeConfig) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.randomizedDue(&a.nextLocalShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
}

func (a *Actor) nextStoreDue(now time.Time, rc robotconfig.RuntimeConfig) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.randomizedDue(&a.nextStore, now, rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
}

func (a *Actor) nextMoveDue(now time.Time, rc robotconfig.RuntimeConfig) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.randomizedDue(&a.nextMove, now, rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec)
}

func (a *Actor) recordFailure(now time.Time) int {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.failures++
	if a.firstFailureAt.IsZero() {
		a.firstFailureAt = now
	}
	return a.failures
}

func (a *Actor) runBusy(kind string, fn func()) {
	a.setBusy(true, kind)
	defer a.setBusy(false, "")
	fn()
}

func (a *Actor) setBusy(v bool, kind string) {
	a.stateMu.Lock()
	a.busy = v
	a.busyKind = kind
	if v {
		a.state = StateBusy
	} else if a.uid > 0 {
		if a.onlineDesired {
			a.state = StateRunning
		} else {
			a.state = StateOffline
		}
	}
	a.stateMu.Unlock()
}

func (a *Actor) busyValue() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.busy
}

func (a *Actor) setState(state State) {
	a.stateMu.Lock()
	a.state = state
	a.stateMu.Unlock()
}

func (a *Actor) snapshot() Snapshot {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return Snapshot{
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

func (a *Actor) uidValue() int {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.uid
}

func (a *Actor) slotIDValue() int {
	if a == nil {
		return 0
	}
	return a.slotID
}

func (a *Actor) modeValue() Mode {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.mode
}

func (a *Actor) stateValue() State {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.state
}

func (a *Actor) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	if a.rand == nil {
		return 0
	}
	return a.rand.Intn(n)
}

func (a *Actor) randBetween(min, max int) int {
	if max < min {
		min, max = max, min
	}
	return min + a.randIntn(max-min+1)
}

func (a *Actor) randomizedDue(next *time.Time, now time.Time, minSec, maxSec int) bool {
	return RandomizedDue(next, now, minSec, maxSec, a.randBetween)
}

func (a *Actor) Status(now time.Time, rc robotconfig.RuntimeConfig) Status {
	return a.status(now, rc)
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

func (a *Actor) Snapshot() Snapshot {
	return a.snapshot()
}

func (a *Actor) UIDValue() int {
	return a.uidValue()
}

func (a *Actor) SlotIDValue() int {
	return a.slotIDValue()
}

func (a *Actor) ModeValue() Mode {
	return a.modeValue()
}

func (a *Actor) Start() {
	a.start()
}

func (a *Actor) ResetForUID(uid int) {
	a.resetForUID(uid)
}

func (a *Actor) SetOnlineDesired(v bool) {
	a.setOnlineDesired(v)
}

func (a *Actor) Tick(now time.Time) {
	a.tick(now)
}

func (a *Actor) MarkOnlinePending(now time.Time) {
	a.markOnlinePending(now)
}

func (a *Actor) MarkOnlineHealthy() {
	a.markOnlineHealthy()
}

func (a *Actor) SetBusyForTest(busy bool) {
	a.stateMu.Lock()
	a.busy = busy
	a.stateMu.Unlock()
}

func (a *Actor) SetStateForTest(state State) {
	a.stateMu.Lock()
	a.state = state
	a.stateMu.Unlock()
}

func (a *Actor) SetFailuresForTest(failures int) {
	a.stateMu.Lock()
	a.failures = failures
	a.stateMu.Unlock()
}

func (a *Actor) SetFirstFailureAtForTest(firstFailureAt time.Time) {
	a.stateMu.Lock()
	a.firstFailureAt = firstFailureAt
	a.stateMu.Unlock()
}

func (a *Actor) SetLastOnlineTryForTest(lastOnlineTry time.Time) {
	a.stateMu.Lock()
	a.lastOnlineTry = lastOnlineTry
	a.stateMu.Unlock()
}

func (a *Actor) SetNextStoreForTest(nextStore time.Time) {
	a.stateMu.Lock()
	a.nextStore = nextStore
	a.stateMu.Unlock()
}
