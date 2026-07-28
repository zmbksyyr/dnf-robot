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
