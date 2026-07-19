package shared

import "testing"

func TestLevelMinExp(t *testing.T) {
	tests := []struct {
		level int
		want  int
		ok    bool
	}{
		{level: 0},
		{level: 1, want: 0, ok: true},
		{level: 2, want: 1000, ok: true},
		{level: 85, want: 929492724, ok: true},
		{level: 86},
	}

	for _, tt := range tests {
		got, ok := LevelMinExp(tt.level)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("LevelMinExp(%d) = (%d, %t), want (%d, %t)", tt.level, got, ok, tt.want, tt.ok)
		}
	}
}
