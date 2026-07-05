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
	if robots[0].UID != 17000000 || robots[0].CID != 900000 || robots[0].Port != 10011 {
		t.Fatalf("first robot got %+v", robots[0])
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
	rc      robotconfig.RuntimeConfig
	created int
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

func (e *testCreateEnv) AvatarFromCatalog(int, int, int, robotconfig.RuntimeConfig) error {
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

func (e *testCreateEnv) EnsureStorePermission(int, int) error { return nil }

func (e *testCreateEnv) EnsureWorldHornByCID(int) error { return nil }

func (e *testCreateEnv) EnsureSchema() error { return nil }

func (e *testCreateEnv) EquipFromCatalog(int, int, int, robotconfig.RuntimeConfig) error {
	return nil
}

func (e *testCreateEnv) LoadMapCatalog() []shared.MapCatalogItem { return nil }

func (e *testCreateEnv) PopulateInventory(robotcap.Info, robotconfig.RuntimeConfig) error {
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
