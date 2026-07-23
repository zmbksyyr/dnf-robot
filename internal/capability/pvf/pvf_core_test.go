package pvf

import (
	"os"
	"path/filepath"
	"testing"

	"robot/internal/capability/catalog"
	"robot/internal/shared"
)

func TestEnsureExportsLoadsExistingSkillCatalogWithoutPVFSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, pvfSkillStateExportName)
	data := []byte(`[{"job":6,"skill_index":3,"state":22,"script_path":"sqr/character/thief/shiningcut.nut","state_data":"AwAA"}]`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(dir, pvfLevelExpExportName), []int{0, 0, 1000, 2653}); err != nil {
		t.Fatal(err)
	}
	setSkillStateCatalog(nil)
	catalog.ClearLevelMinExpTable()
	t.Cleanup(func() {
		setSkillStateCatalog(nil)
		catalog.ClearLevelMinExpTable()
	})

	for _, dfGameR := range []string{"", filepath.Join(dir, "missing", "df_game_r")} {
		setSkillStateCatalog(nil)
		if err := EnsureExports(dfGameR, dir); err != nil {
			t.Fatalf("EnsureExports(%q): %v", dfGameR, err)
		}
		got := shared.SkillStatesForJob(6)
		if len(got) != 1 || got[0].SkillIndex != 3 || got[0].State != 22 || got[0].ScriptPath != "sqr/character/thief/shiningcut.nut" {
			t.Fatalf("loaded skill snapshot for %q = %+v", dfGameR, got)
		}
		if exp, ok := catalog.LevelMinExp(3); !ok || exp != 2653 {
			t.Fatalf("loaded level exp for %q = (%d, %t)", dfGameR, exp, ok)
		}
	}
}

func TestExtractPVFLevelExpIndexesByCharacterLevel(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"character/exptable.tbl": {Name: "character/exptable.tbl", Data: []byte("#PVF_File\r\n1000\t2653\t5543")},
	}}
	got, err := extractPVFLevelExp(archive)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 0, 1000, 2653, 5543}
	if len(got) != len(want) {
		t.Fatalf("level exp length got %d want %d", len(got), len(want))
	}
	for level := range want {
		if got[level] != want[level] {
			t.Fatalf("level %d exp got %d want %d", level, got[level], want[level])
		}
	}
}

func TestPVFExportsCurrentInvalidatesOldSkillStateSchema(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"pvf_equipment_catalog.json": []byte(`[{"item_type": 20}]`),
		"pvf_stackable_catalog.json": []byte(`[{"id": 1}]`),
		"pvf_map_catalog.json":       []byte(`[{"id": 1}]`),
		pvfSkillStateExportName:      []byte(`[{"job": 1}]`),
		pvfLevelExpExportName:        []byte(`[0,0,1000]`),
		pvfItemInfoExportName:        []byte("iteminfo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want := pvfManifest{
		Version: pvfExportVersion, SkillStateVersion: pvfSkillStateExportVersion,
		Source: "/game/Script.pvf", Size: 100, ModTime: 200, MD5: "abc",
	}
	manifestPath := filepath.Join(dir, "pvf_manifest.json")
	old := want
	old.SkillStateVersion = 0
	if err := WriteJSON(manifestPath, old); err != nil {
		t.Fatal(err)
	}
	if pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("old skill state schema was treated as current")
	}
	if err := WriteJSON(manifestPath, want); err != nil {
		t.Fatal(err)
	}
	if !pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("matching skill state schema was not current")
	}
}

func TestPVFExportsCurrentAcceptsMetadataMatchWithStoredMD5(t *testing.T) {
	dir := t.TempDir()
	writeCurrentPVFExportFiles(t, dir)

	got := pvfManifest{
		Version: pvfExportVersion, SkillStateVersion: pvfSkillStateExportVersion,
		Source: "/game/Script.pvf", Size: 100, ModTime: 200, MD5: "abc",
	}
	manifestPath := filepath.Join(dir, "pvf_manifest.json")
	if err := WriteJSON(manifestPath, got); err != nil {
		t.Fatal(err)
	}
	want := got
	want.MD5 = ""
	if !pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("metadata-only match with stored md5 was not current")
	}

	got.MD5 = ""
	if err := WriteJSON(manifestPath, got); err != nil {
		t.Fatal(err)
	}
	if pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("manifest without stored md5 was treated as current")
	}
}

func writeCurrentPVFExportFiles(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{
		"pvf_equipment_catalog.json": []byte(`[{"item_type": 20}]`),
		"pvf_stackable_catalog.json": []byte(`[{"id": 1}]`),
		"pvf_map_catalog.json":       []byte(`[{"id": 1}]`),
		pvfSkillStateExportName:      []byte(`[{"job": 1, "skill_index": 1, "state": 1}]`),
		pvfLevelExpExportName:        []byte(`[0,0,1000]`),
		pvfItemInfoExportName:        []byte("iteminfo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
