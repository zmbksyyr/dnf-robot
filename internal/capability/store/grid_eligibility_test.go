package store

import (
	"testing"

	"robot/internal/shared"
)

func TestGateAreaEligibilityDiffersForNormalAndStoreRoles(t *testing.T) {
	const village, area = 1, 1
	if !IsNormalAreaEligible(village, area) {
		t.Fatal("gate area must be eligible for normal robots")
	}
	if IsStoreAreaEligible(village, area) {
		t.Fatal("gate area must not be eligible for private stores")
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
