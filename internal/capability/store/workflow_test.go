package store

import (
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

func (e *cancellingWorkflowEnv) RestoreAutoNormalOnline(info robotcap.Info, _ robotconfig.RuntimeConfig, _ string) (robotcap.Info, bool) {
	e.restored++
	return info, true
}

func (e *cancellingWorkflowEnv) Logf(string, ...interface{}) {}

func TestAutoStoreCancellationDoesNotLogoutOrRestore(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, Use: true}})
	env := &cancellingWorkflowEnv{points: NewPointCoordinator(configDir, nil)}
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
	if env.finished != 1 || env.logouts != 0 || env.restored != 0 {
		t.Fatalf("cancel cleanup finished=%d logouts=%d restored=%d", env.finished, env.logouts, env.restored)
	}
	if _, ok := env.points.Claim(9); !ok {
		t.Fatal("cancelled attempt kept its store point claimed")
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
