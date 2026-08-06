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

func TestExtractItemListMarksUnsupportedAddCastSpeed(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"equipment/equipment.lst": {
			Name: "equipment/equipment.lst",
			Data: []byte("101030240 `character/swordman/weapon/hsword/101030240.equ`"),
		},
		"equipment/character/swordman/weapon/hsword/101030240.equ": {
			Name: "equipment/character/swordman/weapon/hsword/101030240.equ",
			Data: []byte("[name]\r\n`bad weapon`\r\n[rarity]\r\n1\r\n[equipment type]\r\n`weapon`\r\n[add cast speed]\r\n20\r\n"),
		},
	}}

	items := extractItemList(archive, "equipment/equipment.lst", "equipment/", false)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if !items[0].ClientIncompatible {
		t.Fatal("[add cast speed] equipment was not marked client-incompatible")
	}
}
