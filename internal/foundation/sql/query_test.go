package sql

import "testing"

func TestPlaceholders(t *testing.T) {
	if got := Placeholders(3); got != "?,?,?" {
		t.Fatalf("placeholders got %q want ?,?,?", got)
	}
	if got := Placeholders(0); got != "" {
		t.Fatalf("empty placeholders got %q", got)
	}
}
