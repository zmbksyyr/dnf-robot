package robotspawn

import (
	"testing"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type spawnTestEnv struct{}

func (spawnTestEnv) FollowAccountVillage(string) (int, bool, error) { return 0, false, nil }
func (spawnTestEnv) RandBetween(min, _ int) int                     { return min }
func (spawnTestEnv) RandIntn(int) int                               { return 0 }

func TestConfiguredVillageUsesCurrentPVFCatalogWithoutFixedUpperBound(t *testing.T) {
	info := robotcap.Info{Village: 1, Area: 0, Level: 85}
	rc := robotconfig.Default()
	rc.SpawnFixed = true
	rc.SpawnVillage = 26
	maps := []shared.MapCatalogItem{{Village: 26, Area: 3, Use: true, Rectangles: []shared.MapRectangle{{XMin: 100, XMax: 120, YMin: 200, YMax: 220}}}}
	ApplyConfiguredLocation(spawnTestEnv{}, &info, rc, maps)
	if info.Village != 26 || info.Area != 3 {
		t.Fatalf("configured position=%+v, want PVF village 26 area 3", info)
	}
}

func TestMissingConfiguredVillageKeepsExistingValidPosition(t *testing.T) {
	info := robotcap.Info{Village: 5, Area: 2, X: 10, Y: 20, Level: 85}
	rc := robotconfig.Default()
	rc.SpawnFixed = true
	rc.SpawnVillage = 99
	ApplyConfiguredLocation(spawnTestEnv{}, &info, rc, []shared.MapCatalogItem{{Village: 5, Area: 2, Use: true}})
	if info.Village != 5 || info.Area != 2 || info.X != 10 || info.Y != 20 {
		t.Fatalf("missing configured village changed position: %+v", info)
	}
}

func TestAutomaticSpawnSkipsNonPublicMaps(t *testing.T) {
	allowed, denied := true, false
	maps := []shared.MapCatalogItem{
		{Village: 3, Area: 10, Use: true, NormalEligible: &denied, XMin: 10, XMax: 100, YMin: 20, YMax: 100},
		{Village: 3, Area: 2, Use: true, Gate: true, NormalEligible: &allowed, XMin: 10, XMax: 100, YMin: 20, YMax: 100},
		{Village: 3, Area: 0, Use: true, NormalEligible: &allowed, XMin: 10, XMax: 100, YMin: 20, YMax: 100},
	}
	if got, ok := RandomMap(spawnTestEnv{}, maps, 85); !ok || got.Area != 0 {
		t.Fatalf("random spawn=%+v ok=%v; want public area 0", got, ok)
	}
	info := robotcap.Info{Village: 1, Area: 0, Level: 85}
	rc := robotconfig.Default()
	rc.SpawnArea = 10
	ApplyVillageLocation(spawnTestEnv{}, &info, 3, rc, maps)
	if info.Village != 3 || info.Area != 0 {
		t.Fatalf("configured village selected excluded area: %+v", info)
	}
	targets, ok := DistributedTargets(spawnTestEnv{}, maps, []int{85, 85, 85}, nil)
	if !ok || len(targets) != 3 {
		t.Fatalf("distributed spawn=%+v ok=%v", targets, ok)
	}
	for _, target := range targets {
		if target.Map.Area != 0 {
			t.Fatalf("distributed spawn chose non-public map: %+v", target)
		}
	}
	for _, excluded := range [][]shared.MapCatalogItem{maps[:1], maps[1:2]} {
		if got, ok := RandomMap(spawnTestEnv{}, excluded, 85); ok {
			t.Fatalf("random spawn fell back to excluded map: %+v", got)
		}
		if got, ok := DistributedTargets(spawnTestEnv{}, excluded, []int{85}, nil); ok {
			t.Fatalf("distributed spawn fell back to excluded map: %+v", got)
		}
	}
	legacy := []shared.MapCatalogItem{{Village: 3, Area: 0, Use: true, XMin: 10, XMax: 100, YMin: 20, YMax: 100}}
	if _, ok := RandomMap(spawnTestEnv{}, legacy, 85); !ok {
		t.Fatal("legacy public map with no eligibility marker was rejected")
	}
}
