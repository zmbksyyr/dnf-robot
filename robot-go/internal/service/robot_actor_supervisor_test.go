package service

import (
	"errors"
	"testing"
	"time"
)

func TestSupervisorMaintainsAutoActorSlots(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	s.ensureAutoActorSlots(robotRuntimeConfig{SchedulerOnlineBatchSize: 3}, 3)
	if got := s.actorCounts(time.Now(), robotRuntimeConfig{}).auto; got != 3 {
		t.Fatalf("active actors got %d want 3", got)
	}
	s.ensureAutoActorSlots(robotRuntimeConfig{SchedulerOnlineBatchSize: 3}, 1)
	if got := s.actorCounts(time.Now(), robotRuntimeConfig{}).auto; got != 1 {
		t.Fatalf("active actors after shrink got %d want 1", got)
	}
	s.stopAll(false)
}

func TestSupervisorLeaseUIDSkipsDuplicatesAndBlocked(t *testing.T) {
	ledger := newActorLedger()
	a := &robotActor{slotID: 1}
	if !ledger.tryLeaseUID(101, a) {
		t.Fatalf("first lease should succeed")
	}
	if ledger.tryLeaseUID(101, &robotActor{slotID: 2}) {
		t.Fatalf("duplicate lease should fail")
	}
	ledger.unleaseUID(101, a)
	ledger.blockUID(101)
	if ledger.tryLeaseUID(101, a) {
		t.Fatalf("blocked uid should not lease")
	}
}

func TestSupervisorStopUIDsRemovesActorsAndBlocked(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	a1 := newRobotActor(1, robotActorAuto, s.runtime)
	a2 := newRobotActor(2, robotActorAuto, s.runtime)
	a1.start()
	a2.start()
	addLedgerActor(t, &s.ledger, a1)
	addLedgerActor(t, &s.ledger, a2)
	s.ledger.tryLeaseUID(101, a1)
	s.ledger.tryLeaseUID(102, a2)
	s.ledger.blockUID(101)

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
	if _, ok := runtime.locks.Load(999); ok {
		t.Fatalf("StopUIDs logout=false should not call runtime logout for detached uid")
	}
}

func TestSupervisorActorOwnsUIDWithoutLease(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	a := newRobotActor(1, robotActorAuto, s.runtime)
	a.resetForUID(101)
	addLedgerActor(t, &s.ledger, a)
	if !s.actorOwnsUID(101) {
		t.Fatalf("actorOwnsUID should see uid held by actor even without lease")
	}
	if s.actorOwnsUID(202) {
		t.Fatalf("actorOwnsUID should reject uid not held by actor")
	}
}

func TestRobotActorOfflineKeepsUIDAttached(t *testing.T) {
	a := &robotActor{slotID: 1, mode: robotActorAuto}
	a.resetForUID(101)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired || snap.State != robotActorAssigned {
		t.Fatalf("assigned snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.setOnlineDesired(false)
	a.tick(time.Now())
	if snap := a.snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.State != robotActorOffline {
		t.Fatalf("offline snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.setOnlineDesired(true)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired {
		t.Fatalf("online desired should re-open without detaching uid, uid=%d desired=%v", snap.UID, snap.OnlineDesired)
	}
}

func TestSupervisorAttachUIDUsesEmptyActorSlot(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	registry := newSupervisorActorRegistry(s)
	a := newRobotActor(1, robotActorAuto, s.runtime)
	a.start()
	defer a.stopAndWait(time.Second)
	addLedgerActor(t, &s.ledger, a)

	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should use empty actor")
	}
	if !registry.HasUID(101) {
		t.Fatalf("attached uid should be leased")
	}
	if snap := a.snapshot(); snap.UID != 101 || snap.State != robotActorAssigned {
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
	a := newRobotActor(1, robotActorAuto, s.runtime)
	a.start()
	defer a.stopAndWait(time.Second)
	addLedgerActor(t, &s.ledger, a)

	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should attach uid")
	}
	if !registry.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should be idempotent for leased uid")
	}
	s.ledger.blockUID(102)
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
		run  func() (RobotCommandResult, error)
	}{
		{name: "online", run: func() (RobotCommandResult, error) { return m.OnlineManaged(RobotCommandRequest{Count: 1}) }},
		{name: "move", run: func() (RobotCommandResult, error) { return m.MoveManaged(RobotCommandRequest{Count: 1}) }},
		{name: "shout", run: func() (RobotCommandResult, error) { return m.ShoutManaged(RobotCommandRequest{Count: 1}, false) }},
		{name: "shout_both", run: func() (RobotCommandResult, error) { return m.ShoutBothManaged(RobotCommandRequest{Count: 1}) }},
		{name: "store", run: func() (RobotCommandResult, error) { return m.StoreManaged(RobotCommandRequest{Count: 1}) }},
		{name: "logout", run: func() (RobotCommandResult, error) { return m.LogoutManaged(RobotCommandRequest{Count: 1}) }},
	}
	for _, action := range actions {
		if _, err := action.run(); !errors.Is(err, errActorRegistryUnavailable) {
			t.Fatalf("%s err=%v want actor registry unavailable", action.name, err)
		}
	}
}

func TestActorStatusFieldsDeriveLedgerState(t *testing.T) {
	actor := robotActorSnapshot{State: robotActorOffline, OnlineDesired: false}
	if got := actorOperation(actor); got != "offline" {
		t.Fatalf("offline operation got %q", got)
	}
	actor = robotActorSnapshot{State: robotActorBusy, BusyKind: "store", OnlineDesired: true}
	if got := actorOperation(actor); got != "store" {
		t.Fatalf("busy operation got %q", got)
	}
	if got := actorHealthState("ok", robotActorSnapshot{Failures: 1}); got != "suspect" {
		t.Fatalf("health got %q want suspect", got)
	}
}

func TestRobotActorSnapshotHelpers(t *testing.T) {
	cases := []struct {
		name    string
		snap    robotActorSnapshot
		pending bool
		empty   bool
	}{
		{name: "empty", snap: robotActorSnapshot{}, pending: true, empty: true},
		{name: "offline attached", snap: robotActorSnapshot{UID: 1, State: robotActorOffline}, pending: true},
		{name: "online pending", snap: robotActorSnapshot{UID: 1, State: robotActorOnline}, pending: true},
		{name: "running", snap: robotActorSnapshot{UID: 1, State: robotActorRunning}, pending: false},
		{name: "busy", snap: robotActorSnapshot{UID: 1, State: robotActorBusy}, pending: false},
	}
	for _, tc := range cases {
		if got := tc.snap.schedulerPending(); got != tc.pending {
			t.Fatalf("%s pending got %v want %v", tc.name, got, tc.pending)
		}
		if got := tc.snap.empty(); got != tc.empty {
			t.Fatalf("%s empty got %v want %v", tc.name, got, tc.empty)
		}
	}
}

func TestRobotActorUnhealthyByFailureCount(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 801, failures: 5, firstFailureAt: now.Add(-10 * time.Second)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}
	a.busy = true
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if status.RecycleUID || status.Health != robotActorHealthBusy {
		t.Fatalf("busy actor status got recycle=%v health=%s, want busy without recycle", status.RecycleUID, status.Health)
	}
}

func TestRobotActorBadByFailureCount(t *testing.T) {
	a := &robotActor{mode: robotActorAuto, uid: 1001, failures: 3}
	status := a.status(time.Now(), robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}

	a.failures = 2
	status = a.status(time.Now(), robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle below failure threshold")
	}
}

func TestRobotActorBadByRecoveryWindow(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, failures: 1, firstFailureAt: now.Add(-61 * time.Second)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_window" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_window recycle", status.RecycleUID, status.HealthReason)
	}

	a.firstFailureAt = now.Add(-59 * time.Second)
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle before recovery window expires")
	}

	a.failures = 0
	a.firstFailureAt = now.Add(-61 * time.Second)
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle by failure window without failures, reason=%q", status.HealthReason)
	}
}

func TestRobotActorPendingOnlineUsesConfirmTimeout(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, state: robotActorOnline, lastOnlineTry: now.Add(-61 * time.Second)}
	a.markOnlinePending(now.Add(-61 * time.Second))
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if !status.RecycleUID || status.HealthReason != "online_confirm_timeout" {
		t.Fatalf("pending actor status got recycle=%v reason=%q, want online_confirm_timeout recycle", status.RecycleUID, status.HealthReason)
	}
}

func TestRobotActorHealthyClearsOnlineConfirmTimer(t *testing.T) {
	a := &robotActor{mode: robotActorAuto, uid: 1001, lastOnlineTry: time.Now().Add(-2 * time.Minute), firstFailureAt: time.Now().Add(-2 * time.Minute)}
	a.markOnlineHealthy()
	s := a.snapshot()
	if !s.LastOnlineTry.IsZero() || !s.FirstFailureAt.IsZero() {
		t.Fatalf("healthy actor should clear online timers, got last=%s first_failure=%s", s.LastOnlineTry, s.FirstFailureAt)
	}
}

func TestRobotActorStoreFailedCooldownDoesNotPoisonHealth(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, lastOnlineTry: now.Add(-2 * time.Minute), firstFailureAt: now.Add(-2 * time.Minute), failures: 1}
	a.markOnlineHealthy()
	a.nextStore = now.Add(60 * time.Second)
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID {
		t.Fatalf("store failure cooldown should not recycle actor, reason=%q", status.HealthReason)
	}
	if !status.LastOnlineTry.IsZero() || !status.FirstFailureAt.IsZero() || status.Failures != 0 {
		t.Fatalf("store failure cooldown should clear health timers, got last=%s first=%s failures=%d", status.LastOnlineTry, status.FirstFailureAt, status.Failures)
	}
}

func TestRobotActorOnlineAttemptWithoutPendingDoesNotRecycle(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, state: robotActorOnline, lastOnlineTry: now.Add(-2 * time.Minute)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID {
		t.Fatalf("online attempt without pending marker should not recycle, reason=%q", status.HealthReason)
	}
}
