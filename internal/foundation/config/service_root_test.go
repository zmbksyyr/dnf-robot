package config

import (
	"path/filepath"
	"testing"
)

func TestDNFServiceRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home", "dxf")
	if got := DNFServiceRoot(filepath.Join(root, "game", "df_game_r")); got != root {
		t.Fatalf("service root=%q want %q", got, root)
	}
	if got := DNFServiceRoot("/dp2/df_game_r"); got != "/home/neople" {
		t.Fatalf("fallback service root=%q want /home/neople", got)
	}
}
