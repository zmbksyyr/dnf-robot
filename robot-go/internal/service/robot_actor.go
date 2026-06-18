package service

import (
	"sync"
	"time"
)

type robotActorMode int

const (
	robotActorAuto robotActorMode = iota
)

type robotActorCommand int

const (
	robotActorMove robotActorCommand = iota
	robotActorShoutLocal
	robotActorShoutWorld
	robotActorStore
)

type robotActorState string

const (
	robotActorIdle      robotActorState = "idle"
	robotActorAssigned  robotActorState = "assigned"
	robotActorOnline    robotActorState = "online"
	robotActorRunning   robotActorState = "running"
	robotActorBusy      robotActorState = "busy"
	robotActorReleasing robotActorState = "releasing"
)

type robotActorRequest struct {
	cmd  robotActorCommand
	done chan RobotActionResult
}

type robotActorControlKind int

const (
	robotActorAssign robotActorControlKind = iota
	robotActorRelease
)

type robotActorControl struct {
	kind robotActorControlKind
	uid  int
	done chan robotActorControlResult
}

type robotActorControlResult struct {
	uid int
	ok  bool
}

type robotActorSnapshot struct {
	SlotID         int
	UID            int
	Mode           robotActorMode
	State          robotActorState
	Busy           bool
	BusyKind       string
	LastOnlineTry  time.Time
	FirstFailureAt time.Time
	Failures       int
}

type robotActorHealth string

const (
	robotActorHealthHealthy   robotActorHealth = "healthy"
	robotActorHealthIdle      robotActorHealth = "idle"
	robotActorHealthBusy      robotActorHealth = "busy"
	robotActorHealthUnhealthy robotActorHealth = "unhealthy"
)

type robotActorStatus struct {
	robotActorSnapshot
	Health       robotActorHealth
	HealthReason string
	RecycleUID   bool
}

type robotActor struct {
	slotID  int
	uid     int
	mode    robotActorMode
	state   robotActorState
	runtime *RobotRuntime
	mu      sync.Mutex
	cmds    chan robotActorRequest
	ctrls   chan robotActorControl
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once

	nextMove         time.Time
	nextLocalShout   time.Time
	nextWorldShout   time.Time
	nextStore        time.Time
	storeUntil       time.Time
	lastOnlineTry    time.Time
	firstFailureAt   time.Time
	failures         int
	busy             bool
	busyKind         string
	releaseRequested bool
}

func newRobotActor(slotID int, mode robotActorMode, runtime *RobotRuntime) *robotActor {
	return &robotActor{
		slotID:  slotID,
		mode:    mode,
		state:   robotActorIdle,
		runtime: runtime,
		cmds:    make(chan robotActorRequest, 16),
		ctrls:   make(chan robotActorControl, 4),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (a *robotActor) start() {
	go a.loop()
}

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
	default:
		return robotActorControlResult{}
	}
	if timeout <= 0 {
		res := <-ctrl.done
		return res
	}
	select {
	case res := <-ctrl.done:
		return res
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

func (a *robotActor) tick(now time.Time) {
	uid := a.uidValue()
	if uid <= 0 {
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
	if randomizedDue(&a.nextWorldShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec, a.runtime.manager.randBetween) {
		a.runBusy("shout_world", func() {
			a.runtime.AutoShout(uid, true)
		})
	}
	if randomizedDue(&a.nextLocalShout, now, rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec, a.runtime.manager.randBetween) {
		a.runBusy("shout_local", func() {
			a.runtime.AutoShout(uid, false)
		})
	}
	if !isStore && randomizedDue(&a.nextStore, now, rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec, a.runtime.manager.randBetween) {
		if rc.AutoStoreProbabilityPercent > 0 && a.runtime.manager.randIntn(100) < rc.AutoStoreProbabilityPercent {
			var res RobotActionResult
			a.runBusy("store", func() {
				res = a.runtime.AutoStore(uid, a.releaseRequestedValue)
			})
			a.clearOnlineAttempt()
			if res.OK {
				a.storeUntil = time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second)
			} else if res.State == "store_failed" {
				a.markOnlineHealthy()
				a.nextStore = time.Now().Add(time.Duration(rc.AutoStoreFailCooldownSec) * time.Second)
			} else if res.State != "cancelled" {
				a.recordFailure(time.Now())
			}
			return
		}
	}
	if !isStore && randomizedDue(&a.nextMove, now, rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec, a.runtime.manager.randBetween) {
		a.runBusy("move", func() {
			a.runtime.AutoMove(uid)
		})
	}
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
	if a.onlineConfirmPending(uid, now, rc) {
		a.markOnlinePending(now)
		return
	}
	if now.Sub(a.lastOnlineTryValue()) < time.Duration(rc.ReconnectDelayMS)*time.Millisecond {
		return
	}
	a.lastOnlineTry = now
	a.setState(robotActorOnline)
	res := a.runtime.OnlineNoConfirm(uid, false)
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

func (a *robotActor) releaseCurrentUID() int {
	uid := a.uidValue()
	if uid <= 0 {
		a.setState(robotActorIdle)
		return 0
	}
	a.setState(robotActorReleasing)
	a.runtime.Logout(uid)
	a.resetForUID(0)
	return uid
}

func (a *robotActor) resetForUID(uid int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uid = uid
	a.nextMove = time.Time{}
	a.nextLocalShout = time.Time{}
	a.nextWorldShout = time.Time{}
	a.nextStore = time.Time{}
	a.storeUntil = time.Time{}
	a.lastOnlineTry = time.Time{}
	a.firstFailureAt = time.Time{}
	a.failures = 0
	a.busy = false
	a.busyKind = ""
	a.releaseRequested = false
	if uid > 0 {
		a.state = robotActorAssigned
	} else {
		a.state = robotActorIdle
	}
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
		a.state = robotActorRunning
	}
	a.mu.Unlock()
}

func (a *robotActor) setState(state robotActorState) {
	a.mu.Lock()
	a.state = state
	a.mu.Unlock()
}

func (a *robotActor) status(now time.Time, rc robotRuntimeConfig) robotActorStatus {
	s := a.snapshot()
	status := robotActorStatus{robotActorSnapshot: s, Health: robotActorHealthHealthy}
	if s.Mode != robotActorAuto || s.UID <= 0 {
		status.Health = robotActorHealthIdle
		return status
	}
	if s.Busy {
		status.Health = robotActorHealthBusy
		status.HealthReason = s.BusyKind
		return status
	}
	if s.Failures >= rc.SchedulerBadFailures {
		status.Health = robotActorHealthUnhealthy
		status.HealthReason = "failure_count"
		status.RecycleUID = true
		return status
	}
	if !s.FirstFailureAt.IsZero() && now.Sub(s.FirstFailureAt) >= time.Duration(rc.SchedulerBadRecoverSec)*time.Second {
		status.Health = robotActorHealthUnhealthy
		status.HealthReason = "failure_window"
		status.RecycleUID = true
		return status
	}
	if s.State == robotActorOnline && !s.LastOnlineTry.IsZero() {
		timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if now.Sub(s.LastOnlineTry) > timeout {
			if st, ok := a.runtime.Status(s.UID); !ok || st.StateName != "running" || st.DisconnectReason != 0 {
				status.Health = robotActorHealthUnhealthy
				status.HealthReason = "online_confirm_timeout"
				status.RecycleUID = true
				return status
			}
		}
	}
	return status
}

func (a *robotActor) unhealthy(now time.Time, rc robotRuntimeConfig) bool {
	return a.status(now, rc).RecycleUID
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

func (a *robotActor) handleCommand(cmd robotActorCommand) RobotActionResult {
	uid := a.uidValue()
	if uid <= 0 {
		return RobotActionResult{OK: false, State: "idle", Message: "actor has no uid"}
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
		res := a.runtime.Store(uid)
		if res.OK {
			rc := a.runtime.Config()
			a.storeUntil = time.Now().Add(time.Duration(rc.AutoStoreDurationSec) * time.Second)
		}
		return res
	}
	return RobotActionResult{UID: uid, OK: false, State: "unknown_command"}
}
