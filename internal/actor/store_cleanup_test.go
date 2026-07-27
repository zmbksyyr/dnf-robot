package actor

import (
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
)

type storeCleanupCall struct {
	uid    int
	cid    int
	reason string
}

type storeCleanupRuntime struct {
	*partyWaitRuntime
	cleanups []storeCleanupCall
}

type failedReleaseRuntime struct {
	*partyWaitRuntime
}

func (r *failedReleaseRuntime) Logout(uid int) robotcap.ActionResult {
	return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStatePending}
}

type forceReleaseRuntime struct {
	*failedReleaseRuntime
	forceCalls int
}

func (r *forceReleaseRuntime) ForceClose(int) bool {
	r.forceCalls++
	return true
}

func (r *storeCleanupRuntime) FinishStoreState(uid, cid int, reason string) {
	r.cleanups = append(r.cleanups, storeCleanupCall{uid: uid, cid: cid, reason: reason})
}

func TestLogoutSkipsStoreCleanupForNormalRobot(t *testing.T) {
	runtime := &storeCleanupRuntime{partyWaitRuntime: &partyWaitRuntime{
		status: robotcap.RuntimeStatus{UID: 101, CID: 201, RobotType: 0},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)

	a.logoutCurrentUID()
	if len(runtime.cleanups) != 0 {
		t.Fatalf("normal logout cleanup calls got %d want 0", len(runtime.cleanups))
	}
}

func TestLogoutCleansScheduledStoreState(t *testing.T) {
	runtime := &storeCleanupRuntime{partyWaitRuntime: &partyWaitRuntime{
		status: robotcap.RuntimeStatus{UID: 101, CID: 201, RobotType: 0},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)
	a.setStoreUntil(time.Now().Add(time.Minute))

	a.logoutCurrentUID()
	if len(runtime.cleanups) != 1 || runtime.cleanups[0] != (storeCleanupCall{uid: 101, cid: 201, reason: "logout"}) {
		t.Fatalf("scheduled store cleanup calls got %+v", runtime.cleanups)
	}
}

func TestReleaseSkipsStoreCleanupForNormalRobot(t *testing.T) {
	runtime := &storeCleanupRuntime{partyWaitRuntime: &partyWaitRuntime{
		status: robotcap.RuntimeStatus{UID: 101, CID: 201, RobotType: 0},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)

	a.releaseCurrentUID()
	if len(runtime.cleanups) != 0 {
		t.Fatalf("normal release cleanup calls got %d want 0", len(runtime.cleanups))
	}
}

func TestReleaseCleansRuntimeStoreState(t *testing.T) {
	runtime := &storeCleanupRuntime{partyWaitRuntime: &partyWaitRuntime{
		status: robotcap.RuntimeStatus{UID: 101, CID: 201, RobotType: 3},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)

	a.releaseCurrentUID()
	if len(runtime.cleanups) != 1 || runtime.cleanups[0] != (storeCleanupCall{uid: 101, cid: 201, reason: "release"}) {
		t.Fatalf("runtime store cleanup calls got %+v", runtime.cleanups)
	}
}

func TestReleaseRetainsUIDWhenRuntimeCannotClose(t *testing.T) {
	runtime := &failedReleaseRuntime{partyWaitRuntime: &partyWaitRuntime{
		status: robotcap.RuntimeStatus{UID: 101, StateName: robotcap.RuntimeStateRunning},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)

	if released := a.releaseCurrentUID(); released != 0 {
		t.Fatalf("released uid = %d, want pending", released)
	}
	if snap := a.Snapshot(); snap.UID != 101 || snap.State != StateReleasing {
		t.Fatalf("pending release snapshot = %+v", snap)
	}
}

func TestReleaseForceClosesBeforeDroppingUID(t *testing.T) {
	runtime := &forceReleaseRuntime{failedReleaseRuntime: &failedReleaseRuntime{
		partyWaitRuntime: &partyWaitRuntime{
			status: robotcap.RuntimeStatus{UID: 101, StateName: robotcap.RuntimeStateRunning},
		},
	}}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)

	if released := a.releaseCurrentUID(); released != 101 {
		t.Fatalf("released uid = %d, want 101", released)
	}
	if runtime.forceCalls != 1 {
		t.Fatalf("force close calls = %d, want 1", runtime.forceCalls)
	}
	if snap := a.Snapshot(); snap.UID != 0 || snap.State != StateIdle {
		t.Fatalf("released snapshot = %+v", snap)
	}
}
