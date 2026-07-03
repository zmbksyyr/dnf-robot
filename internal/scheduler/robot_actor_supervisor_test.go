package scheduler

import (
	"errors"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"testing"
	"time"

	actormodel "robot/internal/actor"
)

func TestSupervisorMaintainsAutoActorSlots(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	s.ensureAutoActorSlots(robotconfig.RuntimeConfig{SchedulerOnlineBatchSize: 3}, 3)
	if got := s.actorCounts(time.Now(), robotconfig.RuntimeConfig{}).Auto; got != 3 {
		t.Fatalf("active actors got %d want 3", got)
	}
	s.ensureAutoActorSlots(robotconfig.RuntimeConfig{SchedulerOnlineBatchSize: 3}, 1)
	if got := s.actorCounts(time.Now(), robotconfig.RuntimeConfig{}).Auto; got != 1 {
		t.Fatalf("active actors after shrink got %d want 1", got)
	}
	s.stopAll(false)
}

func TestSupervisorLeaseUIDSkipsDuplicatesAndBlocked(t *testing.T) {
	ledger := actormodel.NewLedger()
	a := testRobotActor(1, 0, 0)
	if !ledger.TryLeaseUID(101, a) {
		t.Fatalf("first lease should succeed")
	}
	if ledger.TryLeaseUID(101, testRobotActor(2, 0, 0)) {
		t.Fatalf("duplicate lease should fail")
	}
	ledger.UnleaseUID(101, a)
	ledger.BlockUID(101)
	if ledger.TryLeaseUID(101, a) {
		t.Fatalf("blocked uid should not lease")
	}
}

func TestSupervisorStopUIDsRemovesActorsAndBlocked(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	a1 := newRobotActor(1, actormodel.ModeAuto, s.runtime)
	a2 := newRobotActor(2, actormodel.ModeAuto, s.runtime)
	a1.Start()
	a2.Start()
	addLedgerActor(t, &s.ledger, a1)
	addLedgerActor(t, &s.ledger, a2)
	s.ledger.TryLeaseUID(101, a1)
	s.ledger.TryLeaseUID(102, a2)
	s.ledger.BlockUID(101)

	if got := registry.StopUIDs([]int{101, 102, 102}, false); got != 2 {
		t.Fatalf("StopUIDs got %d want 2", got)
	}
	if actors, leases := ledgerActorCount(t, &s.ledger), ledgerLeaseCount(t, &s.ledger); actors != 0 || leases != 0 {
		t.Fatalf("StopUIDs should remove actors and leases, actors=%d leases=%d", actors, leases)
	}
	if ledgerIsBlocked(t, &s.ledger, 101) {
		t.Fatalf("StopUIDs should clear blocked marker for removed uid")
	}
}

func TestSupervisorStopUIDsWithoutLogoutSkipsDetachedRuntime(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	runtime := NewRobotRuntime(m)
	s := NewRobotSupervisor(m, runtime)
	registry := newSupervisorActorRegistry(s)
	if got := registry.StopUIDs([]int{999}, false); got != 0 {
		t.Fatalf("StopUIDs got %d want 0 for missing uid", got)
	}
	if runtime.uidLockActive(999) {
		t.Fatalf("StopUIDs logout=false should not call runtime logout for detached uid")
	}
}

func TestRuntimeUIDLocksAreReleasedAfterAction(t *testing.T) {
	runtime := NewRobotRuntime(testRobotManagerWithConfig(t, ""))
	res := runtime.run(101, func() robotcap.ActionResult {
		return robotcap.ActionResult{UID: 101, OK: true}
	})
	if !res.OK {
		t.Fatalf("runtime action should pass")
	}
	if runtime.uidLockActive(101) {
		t.Fatalf("runtime uid lock should be removed after last action")
	}
}

func TestReleaseBrokenLeasesHonorsInterval(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.database = nil
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	a := testRobotActor(1, actormodel.ModeAuto, 101)
	addLedgerActor(t, &s.ledger, a)
	s.ledger.TryLeaseUID(101, a)
	s.nextLeaseHealth = time.Now().Add(time.Minute)

	s.releaseBrokenLeases(time.Now(), robotconfig.RuntimeConfig{SchedulerMetricsIntervalSec: 10})
	if !s.ledger.HasUID(101) {
		t.Fatalf("lease health should be skipped before interval without touching ledger")
	}
}

func TestSupervisorActorOwnsUIDWithoutLease(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	a := newRobotActor(1, actormodel.ModeAuto, s.runtime)
	a.ResetForUID(101)
	addLedgerActor(t, &s.ledger, a)
	if !s.actorOwnsUID(101) {
		t.Fatalf("actorOwnsUID should see uid held by actor even without lease")
	}
	if s.actorOwnsUID(202) {
		t.Fatalf("actorOwnsUID should reject uid not held by actor")
	}
}

func TestRobotActorOfflineKeepsUIDAttached(t *testing.T) {
	a := testRobotActor(1, actormodel.ModeAuto, 0)
	a.ResetForUID(101)
	if snap := a.Snapshot(); snap.UID != 101 || !snap.OnlineDesired || snap.State != actormodel.StateAssigned {
		t.Fatalf("assigned snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.SetOnlineDesired(false)
	a.Tick(time.Now())
	if snap := a.Snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.State != actormodel.StateOffline {
		t.Fatalf("offline snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.SetOnlineDesired(true)
	if snap := a.Snapshot(); snap.UID != 101 || !snap.OnlineDesired {
		t.Fatalf("online desired should re-open without detaching uid, uid=%d desired=%v", snap.UID, snap.OnlineDesired)
	}
}

func TestRobotActorControlReturnsWhenStopped(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	a := newRobotActor(1, actormodel.ModeAuto, NewRobotRuntime(m))
	a.Start()
	a.StopAndWait(time.Second)
	if a.AssignAndWait(101, 0) {
		t.Fatalf("assign on stopped actor should fail instead of blocking")
	}
}

func TestSupervisorAttachUIDUsesEmptyActorSlot(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	a := newRobotActor(1, actormodel.ModeAuto, s.runtime)
	a.Start()
	defer a.StopAndWait(time.Second)
	addLedgerActor(t, &s.ledger, a)

	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should use empty actor")
	}
	if !registry.HasUID(101) {
		t.Fatalf("attached uid should be leased")
	}
	if snap := a.Snapshot(); snap.UID != 101 || snap.State != actormodel.StateAssigned {
		t.Fatalf("actor snapshot uid=%d state=%s", snap.UID, snap.State)
	}
	if registry.AttachUID(102, time.Second) {
		t.Fatalf("AttachUID should fail when no empty actor remains")
	}
}

func TestSupervisorAttachUIDOwnershipBoundaries(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	a := newRobotActor(1, actormodel.ModeAuto, s.runtime)
	a.Start()
	defer a.StopAndWait(time.Second)
	addLedgerActor(t, &s.ledger, a)

	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should attach uid")
	}
	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should be idempotent for leased uid")
	}
	s.ledger.BlockUID(102)
	if registry.AttachUID(102, time.Second) {
		t.Fatalf("AttachUID should reject blocked uid")
	}
}

func TestManagerCurrentActorRegistry(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	if got := m.currentActorRegistry(); got != nil {
		t.Fatalf("empty manager registry got %#v want nil", got)
	}
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	m.supervisor = s
	if got := m.currentActorRegistry(); got == nil {
		t.Fatalf("manager should expose actor registry when supervisor exists")
	}
}

func TestManagedRuntimeActionsRequireActorRegistry(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	actions := []struct {
		name string
		run  func() (robotcap.CommandResult, error)
	}{
		{name: "online", run: func() (robotcap.CommandResult, error) { return m.OnlineManaged(robotcap.CommandRequest{Count: 1}) }},
		{name: "move", run: func() (robotcap.CommandResult, error) { return m.MoveManaged(robotcap.CommandRequest{Count: 1}) }},
		{name: "shout", run: func() (robotcap.CommandResult, error) {
			return m.ShoutManaged(robotcap.CommandRequest{Count: 1}, false)
		}},
		{name: "shout_both", run: func() (robotcap.CommandResult, error) { return m.ShoutBothManaged(robotcap.CommandRequest{Count: 1}) }},
		{name: "store", run: func() (robotcap.CommandResult, error) { return m.StoreManaged(robotcap.CommandRequest{Count: 1}) }},
		{name: "logout", run: func() (robotcap.CommandResult, error) { return m.LogoutManaged(robotcap.CommandRequest{Count: 1}) }},
	}
	for _, action := range actions {
		if _, err := action.run(); !errors.Is(err, errActorRegistryUnavailable) {
			t.Fatalf("%s err=%v want actor registry unavailable", action.name, err)
		}
	}
}

func TestUserActorCommandBusyFollowsAutoAndManualPolicy(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	rc := robotconfig.RuntimeConfig{AutoActions: true, AutoTargetOnlineCount: 2}

	m.autoEnabled = true
	if busy, reason := m.userActorCommandBusy(registry, rc); !busy || reason != "auto_filling actors=0 target=2" {
		t.Fatalf("auto under target busy=%v reason=%q", busy, reason)
	}

	m.autoEnabled = false
	if busy, reason := m.userActorCommandBusy(registry, rc); busy {
		t.Fatalf("manual empty container should be available, reason=%q", reason)
	}
}

func TestUserActorCommandBusyRejectsContainerTransitions(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	rc := robotconfig.RuntimeConfig{AutoActions: false, AutoTargetOnlineCount: 1}
	m.autoEnabled = false

	for _, state := range []actormodel.State{actormodel.StateAssigned, actormodel.StateOnline, actormodel.StateReleasing} {
		s.ledger = actormodel.NewLedger()
		actor := testRobotActorState(1, 101, state)
		addLedgerActor(t, &s.ledger, actor)
		s.ledger.TryLeaseUID(101, actor)
		if busy, reason := m.userActorCommandBusy(registry, rc); !busy || reason != "actor_state="+string(state) {
			t.Fatalf("state %s busy=%v reason=%q", state, busy, reason)
		}
	}
}

func TestRobotActorSnapshotHelpers(t *testing.T) {
	cases := []struct {
		name    string
		snap    actormodel.Snapshot
		pending bool
		empty   bool
	}{
		{name: "empty", snap: actormodel.Snapshot{}, pending: true, empty: true},
		{name: "offline attached", snap: actormodel.Snapshot{UID: 1, State: actormodel.StateOffline}, pending: true},
		{name: "online pending", snap: actormodel.Snapshot{UID: 1, State: actormodel.StateOnline}, pending: true},
		{name: "running", snap: actormodel.Snapshot{UID: 1, State: actormodel.StateRunning}, pending: false},
		{name: "busy", snap: actormodel.Snapshot{UID: 1, State: actormodel.StateBusy}, pending: false},
	}
	for _, tc := range cases {
		if got := actormodel.SnapshotSchedulerPending(tc.snap); got != tc.pending {
			t.Fatalf("%s pending got %v want %v", tc.name, got, tc.pending)
		}
		if got := actormodel.SnapshotEmpty(tc.snap); got != tc.empty {
			t.Fatalf("%s empty got %v want %v", tc.name, got, tc.empty)
		}
	}
}

func TestRobotActorUnhealthyByFailureCount(t *testing.T) {
	now := time.Now()
	a := testRobotActorHealth(801, 5, now.Add(-10*time.Second))
	status := a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}
	testSetActorBusy(a, true)
	status = a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if status.RecycleUID || status.Health != actormodel.HealthBusy {
		t.Fatalf("busy actor status got recycle=%v health=%s, want busy without recycle", status.RecycleUID, status.Health)
	}
}

func TestRobotActorBadByFailureCount(t *testing.T) {
	a := testRobotActorHealth(1001, 3, time.Time{})
	status := a.Status(time.Now(), robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}

	testSetActorFailures(a, 2)
	status = a.Status(time.Now(), robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle below failure threshold")
	}
}

func TestRobotActorBadByRecoveryWindow(t *testing.T) {
	now := time.Now()
	a := testRobotActorHealth(1001, 1, now.Add(-61*time.Second))
	status := a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("single old failure should not recycle, reason=%q", status.HealthReason)
	}

	testSetActorFirstFailureAt(a, now.Add(-59*time.Second))
	status = a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle before recovery window expires")
	}

	testSetActorFailures(a, 0)
	testSetActorFirstFailureAt(a, now.Add(-61*time.Second))
	status = a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle by failure window without failures, reason=%q", status.HealthReason)
	}
}

func TestRobotActorPendingOnlineUsesConfirmTimeout(t *testing.T) {
	now := time.Now()
	a := testRobotActorState(1, 1001, actormodel.StateOnline)
	testSetActorLastOnlineTry(a, now.Add(-61*time.Second))
	a.MarkOnlinePending(now.Add(-61 * time.Second))
	status := a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID || status.HealthReason != "online_confirm_timeout" {
		t.Fatalf("pending actor status got recycle=%v reason=%q, want timeout without recycle", status.RecycleUID, status.HealthReason)
	}
}

func TestRobotActorHealthyClearsOnlineConfirmTimer(t *testing.T) {
	a := testRobotActorHealth(1001, 0, time.Now().Add(-2*time.Minute))
	testSetActorLastOnlineTry(a, time.Now().Add(-2*time.Minute))
	a.MarkOnlineHealthy()
	s := a.Snapshot()
	if !s.LastOnlineTry.IsZero() || !s.FirstFailureAt.IsZero() {
		t.Fatalf("healthy actor should clear online timers, got last=%s first_failure=%s", s.LastOnlineTry, s.FirstFailureAt)
	}
}

func TestRobotActorStoreFailedCooldownDoesNotPoisonHealth(t *testing.T) {
	now := time.Now()
	a := testRobotActorHealth(1001, 1, now.Add(-2*time.Minute))
	testSetActorLastOnlineTry(a, now.Add(-2*time.Minute))
	a.MarkOnlineHealthy()
	testSetActorNextStore(a, now.Add(60*time.Second))
	status := a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID {
		t.Fatalf("store failure cooldown should not recycle actor, reason=%q", status.HealthReason)
	}
	if !status.LastOnlineTry.IsZero() || !status.FirstFailureAt.IsZero() || status.Failures != 0 {
		t.Fatalf("store failure cooldown should clear health timers, got last=%s first=%s failures=%d", status.LastOnlineTry, status.FirstFailureAt, status.Failures)
	}
}

func TestRobotActorOnlineAttemptWithoutPendingStillTimesOut(t *testing.T) {
	now := time.Now()
	a := testRobotActorState(1, 1001, actormodel.StateOnline)
	testSetActorLastOnlineTry(a, now.Add(-2*time.Minute))
	status := a.Status(now, robotconfig.RuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID || status.HealthReason != "online_confirm_timeout" {
		t.Fatalf("online attempt timeout got recycle=%v reason=%q, want timeout without recycle", status.RecycleUID, status.HealthReason)
	}
}
