package mathx

import "testing"

func TestIntHelpers(t *testing.T) {
	if MinInt(2, 1) != 1 {
		t.Fatalf("MinInt failed")
	}
	if MaxInt(2, 1) != 2 {
		t.Fatalf("MaxInt failed")
	}
	if AbsInt(-3) != 3 {
		t.Fatalf("AbsInt failed")
	}
	if BoolToInt(true) != 1 || BoolToInt(false) != 0 {
		t.Fatalf("BoolToInt failed")
	}
}

func TestIntersectRange(t *testing.T) {
	minV, maxV, ok := IntersectRange(10, 1, 3, 7)
	if !ok || minV != 3 || maxV != 7 {
		t.Fatalf("intersect got min=%d max=%d ok=%v", minV, maxV, ok)
	}
	if _, _, ok := IntersectRange(1, 2, 3, 4); ok {
		t.Fatalf("disjoint range should not intersect")
	}
}
