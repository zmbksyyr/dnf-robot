package pvf

import (
	"os"
	"path/filepath"
	"testing"

	"robot/internal/shared"
)

func TestEnsureExportsLoadsExistingSkillCatalogWithoutPVFSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, pvfSkillStateExportName)
	data := []byte(`[{"job":6,"skill_index":3,"state":22,"script_path":"sqr/character/thief/shiningcut.nut","state_data":"AwAA"}]`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	setSkillStateCatalog(nil)
	t.Cleanup(func() { setSkillStateCatalog(nil) })

	for _, dfGameR := range []string{"", filepath.Join(dir, "missing", "df_game_r")} {
		setSkillStateCatalog(nil)
		if err := EnsureExports(dfGameR, dir); err != nil {
			t.Fatalf("EnsureExports(%q): %v", dfGameR, err)
		}
		got := shared.SkillStatesForJob(6)
		if len(got) != 1 || got[0].SkillIndex != 3 || got[0].State != 22 || got[0].ScriptPath != "sqr/character/thief/shiningcut.nut" {
			t.Fatalf("loaded skill snapshot for %q = %+v", dfGameR, got)
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
		pvfItemInfoExportName:        []byte("iteminfo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
