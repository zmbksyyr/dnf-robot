package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"robot/internal/config"
)

var errTestOperation = errors.New("operation failed")

func TestRobotStateName(t *testing.T) {
	tests := map[int]string{
		0:  "stop",
		1:  "init",
		2:  "login",
		3:  "running",
		4:  "clean",
		5:  "wrong",
		99: "unknown",
	}
	for state, want := range tests {
		if got := robotStateName(state); got != want {
			t.Fatalf("state %d: got %q want %q", state, got, want)
		}
	}
}

func TestSummarizeRuntimeStatus(t *testing.T) {
	running, connecting, stores := summarizeRuntimeStatusSlice([]RuntimeRobotStatus{
		{StateName: "running", DisconnectReason: 0},
		{StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		{StateName: "running", DisconnectReason: 1, RobotType: 2, StoreDisplayAck: true},
		{StateName: "init", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 2},
		{StateName: "stop", DisconnectReason: 0},
	})
	if running != 2 {
		t.Fatalf("running got %d want 2", running)
	}
	if connecting != 2 {
		t.Fatalf("connecting got %d want 2", connecting)
	}
	if stores != 1 {
		t.Fatalf("stores got %d want 1", stores)
	}
}

func TestActiveRuntimeStatusRequiresNoDisconnect(t *testing.T) {
	tests := []struct {
		name string
		st   RuntimeRobotStatus
		want bool
	}{
		{name: "running", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 0}, want: true},
		{name: "running disconnected", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 8}, want: false},
		{name: "login", st: RuntimeRobotStatus{StateName: "login", DisconnectReason: 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activeRuntimeStatus(tt.st); got != tt.want {
				t.Fatalf("activeRuntimeStatus() got %v want %v", got, tt.want)
			}
		})
	}
}

func testRobotManagerWithConfig(t *testing.T, robotConfig string) *RobotManager {
	t.Helper()
	configDir := t.TempDir()
	if robotConfig != "" {
		if err := os.WriteFile(filepath.Join(configDir, "robot_config.ini"), []byte(robotConfig), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewRobotManager(nil, &config.SysConfig{ConfigDir: configDir}, nil)
}

func assertIntSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length got %d want %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] got %d want %d: got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func addLedgerActor(t *testing.T, ledger *actorLedger, actor *robotActor) {
	t.Helper()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.actors[actor.slotID] = actor
}

func ledgerActorCount(t *testing.T, ledger *actorLedger) int {
	t.Helper()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.actors)
}

func ledgerLeaseCount(t *testing.T, ledger *actorLedger) int {
	t.Helper()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.uidActors)
}

func ledgerIsBlocked(t *testing.T, ledger *actorLedger, uid int) bool {
	t.Helper()
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	_, ok := ledger.blockedUID[uid]
	return ok
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

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

func TestRobotOperationsTrackRecentStatus(t *testing.T) {
	m := &RobotManager{}
	op := m.BeginOperation("cleanup", "uids=2")
	if op.ID != 1 || op.State != "running" || op.Type != "cleanup" {
		t.Fatalf("begin op got id=%d state=%s type=%s", op.ID, op.State, op.Type)
	}
	done := m.CompleteOperation(op.ID, "deleted=2", nil)
	if done.State != "done" || done.Summary != "deleted=2" || done.FinishedAt.IsZero() {
		t.Fatalf("done op got state=%s summary=%q finished=%s", done.State, done.Summary, done.FinishedAt)
	}
	recent := m.RecentOperation()
	if recent.ID != op.ID || recent.State != "done" {
		t.Fatalf("recent op got id=%d state=%s", recent.ID, recent.State)
	}
	failed := m.BeginOperation("online", "count=1")
	m.CompleteOperation(failed.ID, "", errTestOperation)
	recent = m.RecentOperation()
	if recent.ID != failed.ID || recent.State != "failed" || recent.Error == "" {
		t.Fatalf("failed recent got id=%d state=%s err=%q", recent.ID, recent.State, recent.Error)
	}
}

func TestStructuralOperationGuardRejectsOverlap(t *testing.T) {
	m := &RobotManager{}
	first, err := m.BeginOperationGuarded("cleanup", "all", true)
	if err != nil {
		t.Fatalf("first structural op failed: %v", err)
	}
	if _, err := m.BeginOperationGuarded("create", "count=1", true); err == nil {
		t.Fatalf("second structural op should conflict")
	}
	m.CompleteOperation(first.ID, "done", nil)
	if _, err := m.BeginOperationGuarded("create", "count=1", true); err != nil {
		t.Fatalf("structural op after completion should pass: %v", err)
	}
}

func TestStructuralOperationState(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	done := m.beginStructuralOp("cleanup")
	op, started, active := m.structuralOperation()
	if !active || op != "cleanup" || started.IsZero() {
		t.Fatalf("structural op got active=%v op=%q started=%s", active, op, started)
	}
	done()
	if op, _, active := m.structuralOperation(); active || op != "" {
		t.Fatalf("structural op should clear, active=%v op=%q", active, op)
	}
}

func TestTrackedStructuralOperationMaintainsBothStates(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	op, finish, err := m.beginTrackedStructuralOperation("cleanup", "uids=2")
	if err != nil {
		t.Fatalf("begin tracked structural op failed: %v", err)
	}
	if op.ID == 0 || op.State != "running" {
		t.Fatalf("operation got id=%d state=%s", op.ID, op.State)
	}
	if activeOp, _, active := m.structuralOperation(); !active || activeOp != "cleanup" {
		t.Fatalf("structural op got active=%v op=%q", active, activeOp)
	}
	done := finish("deleted=2", nil)
	if done.State != "done" || done.Summary != "deleted=2" {
		t.Fatalf("finished op got state=%s summary=%q", done.State, done.Summary)
	}
	if activeOp, _, active := m.structuralOperation(); active || activeOp != "" {
		t.Fatalf("structural op should clear, active=%v op=%q", active, activeOp)
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

func TestSelectStoreItemsUsesAllowDenyAndMaterialRules(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 3037, Level: 1, Slot: "material", Trade: true, BasicMaterial: true, Icon: "stackable/material.img", FieldImage: "material/ore", StackLimit: 1000},
		{ID: 3031, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/cloth", StackLimit: 1000},
		{ID: 3032, Level: 99, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/high", StackLimit: 1000},
		{ID: 7312, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/deny", StackLimit: 1000},
		{ID: 3034, Level: 1, Slot: "material", Trade: true, Icon: "stackable/etc.img", FieldImage: "material/bad_icon", StackLimit: 1000},
		{ID: 3035, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 105,
		StoreItemAllowIDs:      []int{3037, 3031, 3032, 3034, 3035, 7312},
		StoreItemDenyIDs:       []int{7312},
	})

	got := storeItemIDSet(items)
	if len(got) != 1 || !got[3037] {
		t.Fatalf("selected IDs got %v want only basic allowed material 3037", got)
	}
}

func TestSelectStoreItemsFallbacksToAllowIDs(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 9001, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/not_allowed", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 105,
		StoreItemAllowIDs:      []int{3037, 3031},
		StoreItemDenyIDs:       []int{3031},
	})

	if len(items) != 1 || items[0].ID != 3037 || items[0].Slot != "material" {
		t.Fatalf("fallback items got %+v want synthetic material 3037", items)
	}
}

func TestStorePointCoordinatorCachesSourceMD5(t *testing.T) {
	configDir := t.TempDir()
	data := writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 80, Use: true}})
	c := newStorePointCoordinator(configDir)
	if len(c.points) == 0 {
		t.Fatalf("expected generated store points")
	}
	cacheData, err := os.ReadFile(filepath.Join(configDir, storePointCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	var cache storePointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	if cache.SourceMD5 != hex.EncodeToString(sum[:]) {
		t.Fatalf("cache md5 got %q want source md5", cache.SourceMD5)
	}
}

func TestStorePointCoordinatorDoesNotReuseFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, false, "test_failed")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("failed point was reused after restart: %s", next.PointID)
	}
}

func TestStorePointCoordinatorExploresUnknownBeforeReusingSuccess(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "test_success")
	second, ok := c.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if second.PointID == first.PointID {
		t.Fatalf("success point reused before unknown points were tried: %s", second.PointID)
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

func testRobotManagerWithStackableCatalog(t *testing.T, catalog []equipmentCatalogItem) *RobotManager {
	t.Helper()
	configDir := t.TempDir()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "pvf_stackable_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return NewRobotManager(nil, &config.SysConfig{ConfigDir: configDir}, nil)
}

func storeItemIDSet(items []equipmentCatalogItem) map[int]bool {
	out := make(map[int]bool, len(items))
	for _, item := range items {
		out[item.ID] = true
	}
	return out
}

func writeStoreMapCatalog(t *testing.T, configDir string, maps []mapCatalogItem) []byte {
	t.Helper()
	data, err := json.Marshal(maps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "pvf_map_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}
