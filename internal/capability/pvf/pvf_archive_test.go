package pvf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPVFRejectsTruncatedFileTreeEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Script.pvf")
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[16:20], 1)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := openPVF(path); err == nil || !strings.Contains(err.Error(), "entry 0 truncated") {
		t.Fatalf("openPVF error = %v, want truncated entry", err)
	}
}
