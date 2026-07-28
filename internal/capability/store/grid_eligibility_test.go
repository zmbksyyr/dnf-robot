package store

import (
	"testing"

	"robot/internal/shared"
)

func eligibility(value bool) *bool { return &value }

func TestGateAreaEligibilityDiffersForNormalAndStoreRoles(t *testing.T) {
	mp := shared.MapCatalogItem{Use: true, Gate: true, NormalEligible: eligibility(true), StoreEligible: eligibility(false)}
	if !IsNormalMapEligible(mp) {
		t.Fatal("gate area must be eligible for normal robots")
	}
	if IsStoreMapEligible(mp) {
		t.Fatal("gate area must not be eligible for private stores")
	}
}

func TestStoreCoordinatorValidatesTargetFromCurrentCatalog(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, StoreEligible: eligibility(true), Rectangles: []shared.MapRectangle{{XMin: 1, XMax: 200, YMin: 1, YMax: 200}}},
		{Village: 1, Area: 1, Use: true, Gate: true, StoreEligible: eligibility(false), Rectangles: []shared.MapRectangle{{XMin: 1, XMax: 200, YMin: 1, YMax: 200}}},
	})
	coordinator := NewPointCoordinator(configDir, nil)
	if !coordinator.HasArea(1, 0) {
		t.Fatal("current PVF store area was not available as a migration target")
	}
	if coordinator.HasArea(1, 1) {
		t.Fatal("gate area became a store migration target")
	}
}

func TestBuildGridPointsRejectsCatalogGateMetadata(t *testing.T) {
	points := BuildGridPoints([]shared.MapCatalogItem{{
		Village: 3, Area: 0, Use: true, Gate: true,
		Rectangles: []shared.MapRectangle{{XMin: 1, XMax: 200, YMin: 1, YMax: 200}},
	}})
	if len(points) != 0 {
		t.Fatalf("gate points=%+v, want none", points)
	}
}

func TestFilterNormalMapsKeepsSafeAreasAndGatesOnly(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, NormalEligible: eligibility(true)},
		{Village: 1, Area: 1, Use: true, Gate: true, NormalEligible: eligibility(true)},
		{Village: 3, Area: 1, Use: true, NormalEligible: eligibility(false)},
		{Village: 2, Area: 0, Use: false},
	}
	got := FilterNormalMaps(maps)
	if len(got) != 2 || got[0].Area != 0 || !got[1].Gate {
		t.Fatalf("normal maps=%+v, want safe area and usable gate only", got)
	}
}
