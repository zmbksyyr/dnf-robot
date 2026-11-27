package pvf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"robot/internal/shared"
)

func TestSkillItemCostFreeRequiresEmptyEntriesInEveryMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		free bool
	}{
		{"absent", "[type] `active`", true},
		{"empty", "[consume item] [/consume item]", true},
		{"empty-modes", "[consume item] [/consume item] [dungeon] [consume item] [/consume item] [/dungeon]", true},
		{"material", "[consume item] 3037 2 2 [/consume item]", false},
		{"zero-count", "[consume item] 3037 0 0 [/consume item]", false},
		{"other-mode", "[pvp] [consume item] 3037 1 1 [/consume item] [/pvp] [dungeon] [consume item] [/consume item] [/dungeon]", false},
		{"orphan", "[/consume item]", false},
		{"unclosed", "[consume item]", false},
		{"unexpected-tag", "[consume item] [name] `value` [/consume item]", false},
		{"case", "[Consume Item] [/consume item]", false},
		{"close-case", "[consume item] [/Consume Item]", false},
		{"literal", "[name] `[consume item]`", true},
		{"empty-file", "", false},
		{"truncated-text", "[name] `unfinished", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := &pvfArchive{files: map[string]*pvfFile{"skill/test.skl": {Data: []byte(tc.text)}}}
			if got := archive.skillItemCostFree("skill/test.skl"); got != tc.free {
				t.Fatalf("item cost free = %t, want %t", got, tc.free)
			}
			if archive.skillItemCostFree("skill/missing.skl") {
				t.Fatal("missing definition supplied free material cost")
			}
		})
	}
}

func TestSkillItemCostFreeRejectsTruncatedBinaryDefinition(t *testing.T) {
	data := []byte{0xb0, 0xd0, 2, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(data[3:], 1)
	archive := &pvfArchive{files: map[string]*pvfFile{"valid": {Data: data}, "truncated": {Data: append(append([]byte(nil), data...), 5)}}}
	if !archive.skillItemCostFree("valid") || archive.skillItemCostFree("truncated") {
		t.Fatal("truncated binary input was treated as a complete free definition")
	}
}

func TestExtractSkillDefinitionsDoesNotLoseConflictingMaterialRows(t *testing.T) {
	for _, list := range []string{"46 `a.skl` 46 `b.skl`", "46 `b.skl` 46 `a.skl`"} {
		archive := &pvfArchive{files: map[string]*pvfFile{
			"skill/swordmanskill.lst": {Data: []byte(list)},
			"skill/a.skl":             {Data: []byte("[type] `active`")},
			"skill/b.skl":             {Data: []byte("[consume item] 3037 1 1 [/consume item]")},
		}}
		definition := extractSkillDefinitions(archive)[[2]int{0, 46}]
		entry := SkillState{}
		applySkillDefinition(&entry, definition)
		if entry.ItemCostFree {
			t.Fatal("duplicate skill identity lost its material requirement")
		}
	}
}

func TestUnavailableSkillCatalogClearsPreviousMaterialProof(t *testing.T) {
	t.Cleanup(func() { setSkillStateCatalog(nil) })
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.json"), invalid} {
		setSkillStateCatalog([]SkillState{{Job: 0, SkillIndex: 46, State: 28, ItemCostFree: true}})
		if err := loadSkillStateCatalog(path); err == nil {
			t.Fatal("invalid catalog loaded")
		}
		if len(shared.SkillStatesForJob(0)) != 0 {
			t.Fatal("failed catalog load retained previous material proof")
		}
	}
	setSkillStateCatalog([]SkillState{{Job: 0, SkillIndex: 46, State: 28, ItemCostFree: true}})
	if err := EnsureExports("", "", dir); err != nil {
		t.Fatal(err)
	}
	if len(shared.SkillStatesForJob(0)) != 0 {
		t.Fatal("disabled PVF retained previous material proof")
	}
}
