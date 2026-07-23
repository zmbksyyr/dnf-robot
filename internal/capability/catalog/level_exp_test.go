package catalog

import "testing"

func TestLevelMinExpUsesLoadedPVFCurve(t *testing.T) {
	ClearLevelMinExpTable()
	t.Cleanup(ClearLevelMinExpTable)
	if err := SetLevelMinExpTable([]int{0, 0, 1000, 2653, 5543}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		level int
		want  int
		ok    bool
	}{
		{level: 0},
		{level: 1, want: 0, ok: true},
		{level: 2, want: 1000, ok: true},
		{level: 4, want: 5543, ok: true},
		{level: 5},
	}
	for _, tt := range tests {
		got, ok := LevelMinExp(tt.level)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("LevelMinExp(%d) = (%d, %t), want (%d, %t)", tt.level, got, ok, tt.want, tt.ok)
		}
	}
}
