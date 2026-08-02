package webadmin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"robot/internal/foundation/atomicfile"
	"robot/internal/foundation/config"
)

func TestWriteMaxUserNumIsAtomicAndIdempotent(t *testing.T) {
	root := t.TempDir()
	dfGameR := filepath.Join(root, "game", "df_game_r")
	cfgDir := filepath.Join(root, "game", "cfg")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(cfgDir, "a.cfg"), filepath.Join(cfgDir, "b.cfg")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("max_user_num = 100\nother = 1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(&config.SysConfig{DFGameR: dfGameR}, "", "")
	matched, changed, err := server.writeMaxUserNum(200)
	if err != nil || !changed || len(matched) != len(paths) {
		t.Fatalf("first update matched=%v changed=%t err=%v", matched, changed, err)
	}
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != "max_user_num = 200\nother = 1\n" {
			t.Fatalf("updated %s data=%q err=%v", path, data, readErr)
		}
	}
	matched, changed, err = server.writeMaxUserNum(200)
	if err != nil || changed || len(matched) != len(paths) {
		t.Fatalf("idempotent update matched=%v changed=%t err=%v", matched, changed, err)
	}
	tmpFiles, err := filepath.Glob(filepath.Join(cfgDir, ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpFiles) != 0 {
		t.Fatalf("atomic temporary files remain: %v", tmpFiles)
	}
}

func TestWriteMaxUserNumRollsBackAllFilesOnFailure(t *testing.T) {
	root := t.TempDir()
	dfGameR := filepath.Join(root, "game", "df_game_r")
	cfgDir := filepath.Join(root, "game", "cfg")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(cfgDir, "a.cfg")
	second := filepath.Join(cfgDir, "b.cfg")
	original := []byte("max_user_num = 100\n")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, original, 0644); err != nil {
			t.Fatal(err)
		}
	}

	oldWriter := writeGameConfigFile
	t.Cleanup(func() { writeGameConfigFile = oldWriter })
	writeGameConfigFile = func(path string, data []byte, mode os.FileMode) error {
		if path == second && bytes.Contains(data, []byte("200")) {
			return errors.New("injected write failure")
		}
		return atomicfile.WriteFile(path, data, mode)
	}

	server := New(&config.SysConfig{DFGameR: dfGameR}, "", "")
	if _, changed, err := server.writeMaxUserNum(200); err == nil || changed {
		t.Fatalf("writeMaxUserNum changed=%t err=%v", changed, err)
	}
	for _, path := range []string{first, second} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("partial update remained in %s: %q", path, data)
		}
	}
}
