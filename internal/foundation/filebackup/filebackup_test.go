package filebackup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveKeepsBoundedNewestFirstCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backups", "target")
	for i := byte(1); i <= 5; i++ {
		if err := Save(path, []byte{i}, 0640, 3); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	for name, want := range map[string][]byte{
		path:                  {5},
		numberedPath(path, 1): {4},
		numberedPath(path, 2): {3},
	} {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
	if _, err := os.Stat(numberedPath(path, 3)); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth copy: %v", err)
	}
}

func TestSaveRejectsInvalidDestination(t *testing.T) {
	if err := Save("", []byte("data"), 0644, 3); err == nil {
		t.Fatal("empty path accepted")
	}
	if err := Save(filepath.Join(t.TempDir(), "target"), []byte("data"), 0644, 0); err == nil {
		t.Fatal("zero backup count accepted")
	}
}
