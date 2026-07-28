package robotspawn

import (
	"testing"

	"robot/internal/shared"
)

type sequenceRangeRandom struct {
	values []int
	index  int
}

func (r *sequenceRangeRandom) RandBetween(min, max int) int {
	if r.index >= len(r.values) {
		return min
	}
	value := r.values[r.index]
	r.index++
	if value < min || value > max {
		return min
	}
	return value
}

func TestRandomPointUsesMovableRectanglesInsteadOfOuterBounds(t *testing.T) {
	mp := shared.MapCatalogItem{
		XMin: 0, XMax: 109, YMin: 0, YMax: 9,
		Rectangles: []shared.MapRectangle{
			{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
			{XMin: 100, XMax: 109, YMin: 0, YMax: 9},
		},
	}
	rng := &sequenceRangeRandom{values: []int{115, 105, 5}}
	x, y, ok := RandomPointInMap(rng, mp)
	if !ok || x != 105 || y != 5 {
		t.Fatalf("point=%d/%d ok=%t, want second rectangle", x, y, ok)
	}
	if x >= 10 && x < 100 {
		t.Fatalf("point=%d/%d landed in outer-bound gap", x, y)
	}
}

func TestIntersectRectanglesKeepsDisconnectedGeometry(t *testing.T) {
	rectangles := []shared.MapRectangle{
		{XMin: 0, XMax: 20, YMin: 0, YMax: 20},
		{XMin: 100, XMax: 120, YMin: 0, YMax: 20},
	}
	got := IntersectRectangles(rectangles, shared.MapRectangle{XMin: 10, XMax: 110, YMin: 5, YMax: 15})
	want := []shared.MapRectangle{
		{XMin: 10, XMax: 20, YMin: 5, YMax: 15},
		{XMin: 100, XMax: 110, YMin: 5, YMax: 15},
	}
	if len(got) != len(want) {
		t.Fatalf("intersections=%+v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("intersection[%d]=%+v want %+v", index, got[index], want[index])
		}
	}
}

func TestNormalizeRectanglesRemovesOverlappingArea(t *testing.T) {
	rectangles := NormalizeRectangles([]shared.MapRectangle{
		{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
		{XMin: 5, XMax: 14, YMin: 0, YMax: 9},
	})
	area := 0
	for _, rectangle := range rectangles {
		area += RectangleArea(rectangle)
	}
	if area != 150 {
		t.Fatalf("normalized rectangles=%+v area=%d, want 150", rectangles, area)
	}
}

func TestRandomPointUsesAreaWeightAndKeepsRandomCoordinates(t *testing.T) {
	rectangles := []shared.MapRectangle{
		{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
		{XMin: 20, XMax: 39, YMin: 0, YMax: 9},
	}
	rng := &sequenceRangeRandom{values: []int{101, 27, 6}}
	x, y, ok := RandomPoint(rng, rectangles)
	if !ok || x != 27 || y != 6 {
		t.Fatalf("point=%d/%d ok=%t, want random point 27/6 in area-weighted second rectangle", x, y, ok)
	}
}

func TestBalancedLocationIgnoresOccupancyOutsideMovableRectangles(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 0, XMax: 9, YMin: 0, YMax: 9}}},
		{Village: 2, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 100, XMax: 109, YMin: 0, YMax: 9}}},
	}
	locations := []shared.MapLocation{{Village: 1, Area: 0, X: 50, Y: 5}}
	target, ok := BalancedLocation(&sequenceRangeRandom{}, maps, 85, locations)
	if !ok {
		t.Fatal("BalancedLocation returned no target")
	}
	if target.Map.Village != 1 {
		t.Fatalf("target=%+v, outer-gap occupancy incorrectly filled village 1", target)
	}
}

func TestBalancedLocationCoversEmptyAreaFirst(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, Rectangles: []shared.MapRectangle{
			{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
			{XMin: 20, XMax: 29, YMin: 0, YMax: 9},
		}},
		{Village: 2, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 100, XMax: 109, YMin: 0, YMax: 9}}},
	}
	locations := []shared.MapLocation{{Village: 1, Area: 0, X: 5, Y: 5}}
	target, ok := BalancedLocation(&sequenceRangeRandom{}, maps, 85, locations)
	if !ok || target.Map.Village != 2 {
		t.Fatalf("target=%+v ok=%t, want empty village 2 before secondary rectangle", target, ok)
	}
}

func TestBalancedLocationDoesNotForceNarrowEmptySlice(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, Rectangles: []shared.MapRectangle{
			{XMin: 0, XMax: 99, YMin: 0, YMax: 99},
			{XMin: 200, XMax: 200, YMin: 0, YMax: 0},
		}},
	}
	locations := []shared.MapLocation{{Village: 1, Area: 0, X: 5, Y: 5}}
	values := make([]int, 0, 24)
	for index := 0; index < 8; index++ {
		values = append(values, 1+index, 10+index*10, 10+index*10)
	}
	target, ok := BalancedLocation(&sequenceRangeRandom{values: values}, maps, 85, locations)
	if !ok || target.X >= 100 {
		t.Fatalf("target=%+v ok=%t, narrow empty geometry slice was forced", target, ok)
	}
}

func TestBalancedLocationUsesAreaWeightAfterRectangleCoverage(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 0, XMax: 9, YMin: 0, YMax: 9}}},
		{Village: 2, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 100, XMax: 199, YMin: 0, YMax: 99}}},
	}
	locations := []shared.MapLocation{
		{Village: 1, Area: 0, X: 5, Y: 5},
		{Village: 2, Area: 0, X: 150, Y: 50},
	}
	target, ok := BalancedLocation(&sequenceRangeRandom{}, maps, 85, locations)
	if !ok || target.Map.Village != 2 {
		t.Fatalf("target=%+v ok=%t, want larger village after full coverage", target, ok)
	}
}

func TestBalancedLocationChoosesCandidateAwayFromCluster(t *testing.T) {
	maps := []shared.MapCatalogItem{{
		Village: 1, Area: 0, Use: true,
		Rectangles: []shared.MapRectangle{{XMin: 0, XMax: 99, YMin: 0, YMax: 99}},
	}}
	locations := []shared.MapLocation{{Village: 1, Area: 0, X: 10, Y: 10}}
	rng := &sequenceRangeRandom{values: []int{
		0,
		1, 11, 11,
		1, 90, 90,
		1, 12, 12,
		1, 13, 13,
		1, 14, 14,
		1, 15, 15,
		1, 16, 16,
		1, 17, 17,
	}}
	target, ok := BalancedLocation(rng, maps, 85, locations)
	if !ok || target.X != 90 || target.Y != 90 {
		t.Fatalf("target=%+v ok=%t, want farthest random candidate 90/90", target, ok)
	}
}
