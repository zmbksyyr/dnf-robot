package pvf

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"robot/internal/shared"
)

func TestCharacterResourcesRequireCompleteTypedSections(t *testing.T) {
	for _, tc := range []struct {
		text  string
		known bool
	}{
		{"", false},
		{"[default reward]\n[/default reward]", true},
		{"[default reward]\n[hp max] `%` 100\n[magical defense] `%` -90\n[/default reward]", true},
		{"[default reward]\n[hp max] `%` 100", false},
		{"[default reward]\n[hp max] `%` 100\n[mp max] `%`\n[/default reward]", false},
		{"[default reward]\n[hp max] `other` 100\n[/default reward]", false},
		{"[default reward]\n[hp max] `%` 100.0\n[/default reward]", false},
		{"[default reward]\n[future stat] `%` 100\n[/default reward]", false},
		{"[default reward]\n[/default reward]\n[default reward]\n[/default reward]", false},
	} {
		tokens, _ := textDefinitionTokens(tc.text)
		if _, known := parseTitlebookDefaultResources(tokens); known != tc.known {
			t.Fatalf("titlebook known=%t want=%t: %s", known, tc.known, tc.text)
		}
	}
	for _, tc := range []struct {
		text  string
		known bool
	}{
		{"[weapon skill info]\n1.0 .95 1.05 1.0\n[/weapon skill info]", true},
		{"[weapon skill info]\n1 1 1 1\n[/weapon skill info]", true},
		{"[weapon skill info]\n1 .95 1.05\n[/weapon skill info]", false},
		{"[weapon skill info]\n1 -1 1 1\n[/weapon skill info]", false},
		{"[weapon skill info]\n1 NaN 1 1\n[/weapon skill info]", false},
		{"[weapon skill info]\n1 `0.95` 1 1\n[/weapon skill info]", false},
	} {
		tokens, _ := textDefinitionTokens(tc.text)
		if _, known := parseWeaponResourceRows(tokens); known != tc.known {
			t.Fatalf("weapon table known=%t want=%t: %s", known, tc.known, tc.text)
		}
	}
}

func TestCharacterResourceExtractionPreservesBinaryFloatBits(t *testing.T) {
	archive := &pvfArchive{files: make(map[string]*pvfFile), stringList: []string{"[weapon skill info]", "[/weapon skill info]"}}
	data := []byte{0xb0, 0xd0}
	for _, cell := range []struct {
		kind  byte
		value uint32
	}{{5, 0}, {4, 0x3f733333}, {4, 0x3f800000}, {4, 0x3f866666}, {4, 0x3f800000}, {5, 1}} {
		var raw [5]byte
		raw[0] = cell.kind
		binary.LittleEndian.PutUint32(raw[1:], cell.value)
		data = append(data, raw[:]...)
	}
	archive.files["character/mage/atmage.chr"] = &pvfFile{Data: data}
	got := extractCharacterResources(archive)
	if len(got.Weapons) != 1 || got.Weapons[0].Job != 8 || math.Float32bits(got.Weapons[0].SkillInfo[0][0]) != 0x3f733333 || math.Float32bits(got.Weapons[0].SkillInfo[0][2]) != 0x3f866666 || got.TitlebookDefaultKnown {
		t.Fatalf("binary character resource extraction=%+v", got)
	}
}

func TestCharacterResourceCatalogLoadsWithoutSourceAndClearsWhenMissing(t *testing.T) {
	t.Cleanup(func() { _ = shared.SetCharacterResourceDefinitions(nil) })
	dir := t.TempDir()
	path := filepath.Join(dir, pvfCharacterResourceExportName)
	definitions := shared.CharacterResourceDefinitions{Version: 1, TitlebookDefaultKnown: true,
		Weapons: []shared.CharacterWeaponResources{{Job: 8, SkillInfo: [][4]float32{{.75, 1, 1.25, 1}}}}}
	if err := WriteJSON(path, definitions); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExports("", dir); err != nil {
		t.Fatal(err)
	}
	if rate, known := shared.CharacterWeaponMPRate(8, 0, 1); !known || rate != .75 {
		t.Fatalf("loaded rate=%g known=%t", rate, known)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExports("", dir); err != nil {
		t.Fatal(err)
	}
	if _, known := shared.CharacterWeaponMPRate(8, 0, 1); known {
		t.Fatal("missing catalog reused the previous source's weapon rates")
	}
	if _, known := shared.TitlebookDefaultResourceBonus(); known {
		t.Fatal("missing catalog reused the previous titlebook reward")
	}
}
