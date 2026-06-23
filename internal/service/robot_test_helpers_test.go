package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"robot/internal/config"
	"testing"
)

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
