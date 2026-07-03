package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"robot/internal/shared"
	"testing"
	"time"

	actormodel "robot/internal/actor"
	"robot/internal/foundation/config"
)

func newRobotActor(slotID int, mode actormodel.Mode, runtime actormodel.RobotRuntime) *actormodel.Actor {
	return actormodel.NewActor(slotID, mode, runtime)
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

func addLedgerActor(t *testing.T, ledger *actormodel.Ledger, actor *actormodel.Actor) {
	t.Helper()
	ledger.AddActorForTest(actor)
}

func testRobotActor(slotID int, mode actormodel.Mode, uid int) *actormodel.Actor {
	a := newRobotActor(slotID, mode, nil)
	a.ResetForUID(uid)
	if uid <= 0 {
		a.SetStateForTest(actormodel.StateIdle)
	}
	return a
}

func testRobotActorState(slotID int, uid int, state actormodel.State) *actormodel.Actor {
	a := testRobotActor(slotID, actormodel.ModeAuto, uid)
	a.SetStateForTest(state)
	return a
}

func testRobotActorHealth(uid int, failures int, firstFailureAt time.Time) *actormodel.Actor {
	a := testRobotActor(1, actormodel.ModeAuto, uid)
	a.SetFailuresForTest(failures)
	a.SetFirstFailureAtForTest(firstFailureAt)
	return a
}

func testSetActorBusy(a *actormodel.Actor, busy bool) {
	a.SetBusyForTest(busy)
}

func testSetActorFailures(a *actormodel.Actor, failures int) {
	a.SetFailuresForTest(failures)
}

func testSetActorFirstFailureAt(a *actormodel.Actor, firstFailureAt time.Time) {
	a.SetFirstFailureAtForTest(firstFailureAt)
}

func testSetActorLastOnlineTry(a *actormodel.Actor, lastOnlineTry time.Time) {
	a.SetLastOnlineTryForTest(lastOnlineTry)
}

func testSetActorNextStore(a *actormodel.Actor, nextStore time.Time) {
	a.SetNextStoreForTest(nextStore)
}

func ledgerActorCount(t *testing.T, ledger *actormodel.Ledger) int {
	t.Helper()
	return ledger.ActorCountForTest()
}

func ledgerLeaseCount(t *testing.T, ledger *actormodel.Ledger) int {
	t.Helper()
	return ledger.LeaseCountForTest()
}

func ledgerIsBlocked(t *testing.T, ledger *actormodel.Ledger, uid int) bool {
	t.Helper()
	return ledger.IsBlockedForTest(uid)
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func writeStoreMapCatalog(t *testing.T, configDir string, maps []shared.MapCatalogItem) []byte {
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
