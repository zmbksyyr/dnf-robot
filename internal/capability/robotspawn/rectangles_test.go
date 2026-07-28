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
	rng := &sequenceRangeRandom{values: []int{15, 105, 5}}
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

func TestBalancedLocationCoversEmptyAreaBeforeEmptyRectangle(t *testing.T) {
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

func TestBalancedLocationCoversGlobalEmptyRectangleAfterAreas(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Use: true, Rectangles: []shared.MapRectangle{
			{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
			{XMin: 20, XMax: 29, YMin: 0, YMax: 9},
		}},
		{Village: 2, Area: 0, Use: true, Rectangles: []shared.MapRectangle{{XMin: 100, XMax: 109, YMin: 0, YMax: 9}}},
	}
	locations := []shared.MapLocation{
		{Village: 1, Area: 0, X: 5, Y: 5},
		{Village: 2, Area: 0, X: 105, Y: 5},
	}
	target, ok := BalancedLocation(&sequenceRangeRandom{}, maps, 85, locations)
	want := maps[0].Rectangles[1]
	if !ok || target.Map.Village != 1 || target.Rectangle != want {
		t.Fatalf("target=%+v ok=%t, want global empty rectangle %+v", target, ok, want)
	}
}

func TestBalancedLocationEmptyRectangleRespectsLevel(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 0, Level: 1, Use: true, Rectangles: []shared.MapRectangle{
			{XMin: 0, XMax: 9, YMin: 0, YMax: 9},
			{XMin: 20, XMax: 29, YMin: 0, YMax: 9},
		}},
		{Village: 2, Area: 0, Level: 85, Use: true, Rectangles: []shared.MapRectangle{
			{XMin: 100, XMax: 109, YMin: 0, YMax: 9},
			{XMin: 120, XMax: 129, YMin: 0, YMax: 9},
		}},
	}
	locations := []shared.MapLocation{
		{Village: 1, Area: 0, X: 5, Y: 5},
		{Village: 2, Area: 0, X: 105, Y: 5},
	}
	target, ok := BalancedLocation(&sequenceRangeRandom{}, maps, 50, locations)
	want := maps[0].Rectangles[1]
	if !ok || target.Map.Village != 1 || target.Rectangle != want {
		t.Fatalf("target=%+v ok=%t, low-level role must use eligible empty rectangle %+v", target, ok, want)
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
