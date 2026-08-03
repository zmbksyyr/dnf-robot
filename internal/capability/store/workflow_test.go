package store

import (
	"errors"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
	"testing"
)

type cancellingWorkflowEnv struct {
	WorkflowEnv
	points   *PointCoordinator
	finished int
	logouts  int
	restored int
	started  int
}

func (e *cancellingWorkflowEnv) StoreTitle(uid int, rc robotconfig.RuntimeConfig) string {
	return fallbackStoreTitle(uid)
}

func (e *cancellingWorkflowEnv) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	return nil, nil
}

func (e *cancellingWorkflowEnv) BeginStoreBusy(int) bool { return true }

func (e *cancellingWorkflowEnv) AcquireAutoStoreSlot(robotconfig.RuntimeConfig) (func(), bool) {
	return func() {}, true
}

func (e *cancellingWorkflowEnv) EndStoreBusy(int) {}

func (e *cancellingWorkflowEnv) StorePoints() *PointCoordinator { return e.points }

func (e *cancellingWorkflowEnv) RobotGamePort() int { return 10011 }

func (e *cancellingWorkflowEnv) StartPrivateStore(int, string) bool {
	e.started++
	return true
}

func (e *cancellingWorkflowEnv) FinishStoreState(int, int, string) { e.finished++ }

func (e *cancellingWorkflowEnv) Logout(robotcap.CommandRequest) (robotcap.CommandResult, error) {
	e.logouts++
	return robotcap.CommandResult{}, nil
}

func (e *cancellingWorkflowEnv) OfflineCharacterForWrite(int, func() bool) (bool, error) {
	return true, nil
}

func (e *cancellingWorkflowEnv) RestoreAutoNormalOnline(info robotcap.Info, _ robotconfig.RuntimeConfig, _ string) (robotcap.Info, bool) {
	e.restored++
	return info, true
}

func (e *cancellingWorkflowEnv) Logf(string, ...interface{}) {}

func TestAutoStoreCancellationRestoresAfterOfflineBarrier(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, Use: true}})
	env := &cancellingWorkflowEnv{points: newTestPointCoordinator(configDir, nil)}
	checks := 0
	shouldStop := func() bool {
		checks++
		return checks >= 2
	}

	got := (Workflow{Env: env}).AutoUntilSuccess(
		robotcap.RuntimeStatus{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{AutoStoreMaxPositionTries: 1},
		shouldStop,
	)
	if got != AutoAttemptCancelled {
		t.Fatalf("attempt state = %d, want cancelled", got)
	}
	if env.finished != 1 || env.logouts != 0 || env.restored != 1 {
		t.Fatalf("cancel cleanup finished=%d logouts=%d restored=%d", env.finished, env.logouts, env.restored)
	}
	if _, ok := env.points.Claim(9); !ok {
		t.Fatal("cancelled attempt kept its store point claimed")
	}
}

type delayedLogoutWorkflowEnv struct {
	WorkflowEnv
	waited bool
}

func (e *delayedLogoutWorkflowEnv) StoreTitle(uid int, rc robotconfig.RuntimeConfig) string {
	return fallbackStoreTitle(uid)
}

func (*delayedLogoutWorkflowEnv) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	return []robotcap.Info{{UID: 7, CID: 8}}, nil
}

func (*delayedLogoutWorkflowEnv) Config() robotconfig.RuntimeConfig {
	return robotconfig.RuntimeConfig{StoreConfirmTimeoutSec: 1}
}

func (*delayedLogoutWorkflowEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return map[int]robotcap.RuntimeStatus{7: {
		UID:             7,
		CID:             8,
		StateName:       robotcap.RuntimeStateRunning,
		StoreDisplayAck: true,
	}}
}

func (*delayedLogoutWorkflowEnv) BeginStoreBusy(int) bool { return true }

func (*delayedLogoutWorkflowEnv) EndStoreBusy(int) {}

func (*delayedLogoutWorkflowEnv) Logout(robotcap.CommandRequest) (robotcap.CommandResult, error) {
	return robotcap.CommandResult{Accepted: 1}, nil
}

func (e *delayedLogoutWorkflowEnv) WaitAccountOffline(int, func() bool) (bool, error) {
	e.waited = true
	return false, nil
}

func (*delayedLogoutWorkflowEnv) EnsureStoreInventoryAndStall(robotcap.Info, robotconfig.RuntimeConfig) error {
	return nil
}

func (*delayedLogoutWorkflowEnv) InvalidateCharacterCache(int) error { return nil }

func (*delayedLogoutWorkflowEnv) Online(robotcap.CommandRequest, bool) (robotcap.CommandResult, error) {
	return robotcap.CommandResult{Robots: []robotcap.ActionResult{{UID: 7, CID: 8, OK: true, State: robotcap.ActionStateRunning}}}, nil
}

func (*delayedLogoutWorkflowEnv) StartPrivateStore(int, string) bool { return true }

func (*delayedLogoutWorkflowEnv) MarkStoreStarted(int) error { return nil }

func (*delayedLogoutWorkflowEnv) FinishStoreState(int, int, string) {}

func (*delayedLogoutWorkflowEnv) Logf(string, ...interface{}) {}

func TestManualStoreUsesOfflineBoundaryAfterAcceptedLogout(t *testing.T) {
	env := &delayedLogoutWorkflowEnv{}
	result, err := (Workflow{Env: env}).Store(robotcap.CommandRequest{UIDs: []int{7}})
	if err != nil {
		t.Fatal(err)
	}
	if !env.waited {
		t.Fatal("accepted logout did not continue to the offline boundary")
	}
	if result.Confirmed != 1 || result.Failed != 0 {
		t.Fatalf("store result=%+v, want one confirmed store", result)
	}
}

func TestStartAndWaitDisplayCancellationDoesNotLogout(t *testing.T) {
	env := &cancellingWorkflowEnv{}
	ok, reason := (Workflow{Env: env}).startAndWaitDisplay(
		robotcap.Info{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{},
		1,
		func() bool { return true },
	)
	if ok || reason != StoreReasonCancelled {
		t.Fatalf("startAndWaitDisplay() = %v, %q; want cancelled", ok, reason)
	}
	if env.started != 1 || env.logouts != 0 || env.finished != 0 {
		t.Fatalf("wait cancellation started=%d logouts=%d finished=%d", env.started, env.logouts, env.finished)
	}
}

type inventoryRetryWorkflowEnv struct {
	WorkflowEnv
	started       int
	selected      int
	prepared      int
	invalidated   int
	reselected    int
	offline       int
	online        int
	prepareErr    error
	invalidateErr error
	events        []string
}

func (e *inventoryRetryWorkflowEnv) StoreTitle(uid int, rc robotconfig.RuntimeConfig) string {
	return fallbackStoreTitle(uid)
}

func (e *inventoryRetryWorkflowEnv) StartPrivateStore(int, string) bool {
	e.started++
	return true
}

func (e *inventoryRetryWorkflowEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	status := robotcap.RuntimeStatus{UID: 7, StateName: robotcap.RuntimeStateRunning}
	if e.started == 1 {
		status.StoreDisplayRejected = true
	} else {
		status.StoreDisplayAck = true
	}
	return map[int]robotcap.RuntimeStatus{7: status}
}

func (e *inventoryRetryWorkflowEnv) OfflineCharacterForWrite(int, func() bool) (bool, error) {
	e.offline++
	e.events = append(e.events, "offline")
	return false, nil
}

func (e *inventoryRetryWorkflowEnv) RefreshCharacterForWrite(int, func() bool) (bool, error) {
	e.selected++
	e.events = append(e.events, "select")
	return false, nil
}

func (e *inventoryRetryWorkflowEnv) ResumeCharacterAfterWrite(int, func() bool) (bool, error) {
	e.reselected++
	e.events = append(e.events, "reselect")
	return false, nil
}

func (e *inventoryRetryWorkflowEnv) PrepareStorePosition(robotcap.Info) error {
	e.events = append(e.events, "position")
	return nil
}

func (e *inventoryRetryWorkflowEnv) SyncRobotCharacterVillage(int, int) error {
	e.events = append(e.events, "village")
	return nil
}

func (e *inventoryRetryWorkflowEnv) EnsureStoreInventoryAndStall(robotcap.Info, robotconfig.RuntimeConfig) error {
	e.prepared++
	e.events = append(e.events, "inventory")
	return e.prepareErr
}

func (e *inventoryRetryWorkflowEnv) InvalidateCharacterCache(int) error {
	e.invalidated++
	e.events = append(e.events, "invalidate")
	return e.invalidateErr
}

func (e *inventoryRetryWorkflowEnv) Online(robotcap.CommandRequest, bool) (robotcap.CommandResult, error) {
	e.online++
	e.events = append(e.events, "online")
	return robotcap.CommandResult{Confirmed: 1}, nil
}

func (e *inventoryRetryWorkflowEnv) WaitCharacterRunning(int, func() bool) (bool, error) {
	e.events = append(e.events, "running")
	return false, nil
}

func (*inventoryRetryWorkflowEnv) Logf(string, ...interface{}) {}

func TestStartAndWaitDisplayRepreparesOnceWhenInventoryIsNotReady(t *testing.T) {
	env := &inventoryRetryWorkflowEnv{}
	ok, reason := (Workflow{Env: env}).startAndWaitDisplay(
		robotcap.Info{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{StoreConfirmTimeoutSec: 1},
		1,
		nil,
	)
	if !ok || reason != "" {
		t.Fatalf("startAndWaitDisplay() = %v, %q; want success after inventory refresh", ok, reason)
	}
	if env.started != 2 || env.selected != 1 || env.prepared != 1 || env.invalidated != 1 || env.reselected != 1 {
		t.Fatalf("retry calls started=%d selected=%d prepared=%d invalidated=%d reselected=%d, want 2/1/1/1/1", env.started, env.selected, env.prepared, env.invalidated, env.reselected)
	}
	if env.offline != 0 || env.online != 0 {
		t.Fatalf("inventory refresh used full logout/login: offline=%d online=%d", env.offline, env.online)
	}
}

func TestInventoryRefreshReselectsCharacterAfterPrepareFailure(t *testing.T) {
	env := &inventoryRetryWorkflowEnv{prepareErr: errors.New("write failed")}
	reason := (Workflow{Env: env}).refreshAutoInventorySession(
		robotcap.Info{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{},
		nil,
	)
	if reason != StoreReasonPrepareFailed {
		t.Fatalf("refresh reason=%q, want %q", reason, StoreReasonPrepareFailed)
	}
	if env.selected != 1 || env.prepared != 1 || env.invalidated != 0 || env.reselected != 1 {
		t.Fatalf("refresh calls selected=%d prepared=%d invalidated=%d reselected=%d, want 1/1/0/1", env.selected, env.prepared, env.invalidated, env.reselected)
	}
}

func TestPrepareAutoSessionInvalidatesAfterWritesBeforeOnline(t *testing.T) {
	env := &inventoryRetryWorkflowEnv{}
	reason := (Workflow{Env: env}).prepareAutoSession(
		robotcap.Info{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{},
		nil,
	)
	if reason != "" {
		t.Fatalf("prepare reason=%q, want success", reason)
	}
	want := []string{"offline", "position", "village", "inventory", "invalidate", "online", "running"}
	if len(env.events) != len(want) {
		t.Fatalf("events=%v, want %v", env.events, want)
	}
	for i := range want {
		if env.events[i] != want[i] {
			t.Fatalf("events=%v, want %v", env.events, want)
		}
	}
}

func TestInventoryRefreshReselectsAfterCacheInvalidationFailure(t *testing.T) {
	env := &inventoryRetryWorkflowEnv{invalidateErr: errors.New("monitor unavailable")}
	reason := (Workflow{Env: env}).refreshAutoInventorySession(
		robotcap.Info{UID: 7, CID: 8},
		robotconfig.RuntimeConfig{},
		nil,
	)
	if reason != StoreReasonPrepareFailed {
		t.Fatalf("refresh reason=%q, want %q", reason, StoreReasonPrepareFailed)
	}
	if env.invalidated != 1 || env.reselected != 1 {
		t.Fatalf("refresh invalidated=%d reselected=%d, want 1/1", env.invalidated, env.reselected)
	}
}
