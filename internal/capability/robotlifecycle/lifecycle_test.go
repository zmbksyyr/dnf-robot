package robotlifecycle

import (
	"errors"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
	"testing"
)

func TestCreatorCapsCountAndCreatesRobots(t *testing.T) {
	env := &testCreateEnv{rc: robotconfig.RuntimeConfig{
		RobotUIDStart:        17000000,
		RobotUIDEnd:          17999999,
		LevelMin:             10,
		LevelMax:             10,
		Jobs:                 []int{1},
		GrowTypes:            []int{2},
		SpawnFallbackVillage: 1,
		SpawnArea:            2,
		SpawnXMin:            100,
		SpawnXMax:            100,
		SpawnYMin:            200,
		SpawnYMax:            200,
	}}
	robots, err := (Creator{Env: env}).Create(robotcap.CreateRequest{Count: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(robots) != 200 || env.created != 200 {
		t.Fatalf("created got robots=%d env=%d want 200", len(robots), env.created)
	}
	if env.catalogLoads != 1 || env.equipCalls != 200 || env.avatarCalls != 200 || env.inventoryCalls != 200 || env.catalogMismatches != 0 {
		t.Fatalf("catalog reuse got loads=%d equip=%d avatar=%d inventory=%d mismatches=%d", env.catalogLoads, env.equipCalls, env.avatarCalls, env.inventoryCalls, env.catalogMismatches)
	}
	if robots[0].UID != 17000000 || robots[0].CID != 900000 || robots[0].Port != 10011 {
		t.Fatalf("first robot got %+v", robots[0])
	}
}

func TestCreatorSpawnMapsCoverAreasThenUseSmoothedAreaWeight(t *testing.T) {
	env := &testCreateEnv{
		rc: robotconfig.RuntimeConfig{
			RobotUIDStart:        17000000,
			RobotUIDEnd:          17999999,
			LevelMin:             1,
			LevelMax:             1,
			Jobs:                 []int{1},
			GrowTypes:            []int{2},
			SpawnFallbackVillage: 1,
			SpawnArea:            -1,
			SpawnXMin:            100,
			SpawnXMax:            100,
			SpawnYMin:            200,
			SpawnYMax:            200,
		},
		maps: []shared.MapCatalogItem{
			{Village: 1, Area: 1, XMin: 0, XMax: 99, YMin: 0, YMax: 99, Use: true},
			{Village: 1, Area: 2, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true},
			{Village: 1, Area: 3, XMin: 0, XMax: 9, YMin: 0, YMax: 9, Use: true},
		},
	}
	robots, err := (Creator{Env: env}).Create(robotcap.CreateRequest{Count: 4})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int{}
	for _, info := range robots {
		seen[info.Area]++
	}
	if len(seen) != 3 {
		t.Fatalf("areas seen = %v, want all 3 areas covered", seen)
	}
	if seen[1] != 2 {
		t.Fatalf("large area assignments = %d, want one extra assignment after coverage", seen[1])
	}
}

func TestCleanerDryRunAndForce(t *testing.T) {
	env := &testCleanupEnv{candidates: []robotcap.CleanupCandidate{
		{UID: 1, CID: 11},
		{UID: 2, CID: 22, Protected: true},
	}}
	dryRun, err := (Cleaner{Env: env}).Cleanup(robotcap.CleanupRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !dryRun.DryRun || dryRun.Requested != 2 || dryRun.Skipped != 1 || dryRun.Deleted != 0 {
		t.Fatalf("dry run got %+v", dryRun)
	}

	forced, err := (Cleaner{Env: env}).Cleanup(robotcap.CleanupRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if forced.Deleted != 1 || forced.Skipped != 1 || len(env.deletedUIDs) != 1 || env.deletedUIDs[0] != 1 || !env.finished {
		t.Fatalf("forced got result=%+v env=%+v", forced, env)
	}
}

type testCreateEnv struct {
	rc                robotconfig.RuntimeConfig
	created           int
	catalogLoads      int
	equipCalls        int
	avatarCalls       int
	inventoryCalls    int
	catalogMismatches int
	equipmentBase     *shared.EquipmentCatalogItem
	stackableBase     *shared.EquipmentCatalogItem
	maps              []shared.MapCatalogItem
}

func (e *testCreateEnv) AllocateRobotIDs(count, uidStart, uidEnd int) (RobotIDAllocation, error) {
	uids := make([]int, count)
	for i := range uids {
		if uidStart+i > uidEnd {
			return RobotIDAllocation{}, errors.New("uid segment exhausted")
		}
		uids[i] = uidStart + i
	}
	return RobotIDAllocation{UIDs: uids, FirstCID: 900000}, nil
}

func (e *testCreateEnv) AvatarFromCatalog(_ int, _ int, _ int, _ robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error {
	e.avatarCalls++
	if len(items) != 1 || items[0].ID != 1001 {
		e.catalogMismatches++
	} else if e.equipmentBase != &items[0] {
		e.catalogMismatches++
	}
	return nil
}

func (e *testCreateEnv) ApplyConfiguredLocation(*robotcap.Info, robotconfig.RuntimeConfig, []shared.MapCatalogItem) {
}

func (e *testCreateEnv) Config() robotconfig.RuntimeConfig { return e.rc }

func (e *testCreateEnv) CopyTemplateDefaults(int) error { return nil }

func (e *testCreateEnv) CreateBaseCharacter(robotcap.Info, robotconfig.RuntimeConfig) error {
	return nil
}

func (e *testCreateEnv) EnsureAccount(int, string) error { return nil }

func (e *testCreateEnv) EnsureWorldHornByCID(int) error { return nil }

func (e *testCreateEnv) EnsureSchema() error { return nil }

func (e *testCreateEnv) EquipFromCatalog(_ int, _ int, _ int, _ robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error {
	e.equipCalls++
	if len(items) != 1 || items[0].ID != 1001 {
		e.catalogMismatches++
	} else if e.equipmentBase == nil {
		e.equipmentBase = &items[0]
	} else if e.equipmentBase != &items[0] {
		e.catalogMismatches++
	}
	return nil
}

func (e *testCreateEnv) LoadCreateCatalogs() CreateCatalogs {
	e.catalogLoads++
	return CreateCatalogs{
		Equipment: []shared.EquipmentCatalogItem{{ID: 1001}},
		Stackable: []shared.EquipmentCatalogItem{{ID: 2001}},
	}
}

func (e *testCreateEnv) LoadMapCatalog() []shared.MapCatalogItem { return e.maps }

func (e *testCreateEnv) PopulateInventory(_ robotcap.Info, _ robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error {
	e.inventoryCalls++
	if len(items) != 1 || items[0].ID != 2001 {
		e.catalogMismatches++
	} else if e.stackableBase == nil {
		e.stackableBase = &items[0]
	} else if e.stackableBase != &items[0] {
		e.catalogMismatches++
	}
	return nil
}

func (e *testCreateEnv) RebuildCharacView(int) error { return nil }

func (e *testCreateEnv) RegisterRobot(robotcap.Info) error {
	e.created++
	return nil
}

func (e *testCreateEnv) RandomFrom(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	return vals[0]
}

func (e *testCreateEnv) RandomMap([]shared.MapCatalogItem, int) (shared.MapCatalogItem, bool) {
	return shared.MapCatalogItem{}, false
}

func (e *testCreateEnv) RandBetween(min, max int) int { return min }

func (e *testCreateEnv) RobotGamePort() int { return 10011 }

func (e *testCreateEnv) RobotInnerIP() string { return "127.0.0.1" }

func (e *testCreateEnv) RobotName(uid int, used map[string]struct{}, rc robotconfig.RuntimeConfig) string {
	return "robot"
}

func (e *testCreateEnv) UpsertDummy(robotcap.Info, string) error { return nil }

type testCleanupEnv struct {
	candidates  []robotcap.CleanupCandidate
	deletedUIDs []int
	finished    bool
}

func (e *testCleanupEnv) BatchDeleteRobotData(uids, cids []int) error {
	if len(uids) == 0 {
		return errors.New("empty delete")
	}
	e.deletedUIDs = append([]int(nil), uids...)
	return nil
}

func (e *testCleanupEnv) CleanupCandidates(robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, error) {
	return append([]robotcap.CleanupCandidate(nil), e.candidates...), nil
}

func (e *testCleanupEnv) EnsureSchema() error { return nil }

func (e *testCleanupEnv) PrepareDelete([]int) func() {
	return func() { e.finished = true }
}
