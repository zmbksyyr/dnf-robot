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
