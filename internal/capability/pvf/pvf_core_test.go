package pvf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	t.Cleanup(func() {
		setSkillStateCatalog(nil)
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

func TestExtractPVFLevelExpStopsAtFirstDecrease(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"character/exptable.tbl": {Name: "character/exptable.tbl", Data: []byte("#PVF_File\r\n1000\t2653\t5543\t100\t200")},
	}}
	got, err := extractPVFLevelExp(archive)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 0, 1000, 2653, 5543}
	if len(got) != len(want) {
		t.Fatalf("level exp length got %d want %d: %v", len(got), len(want), got)
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
		"equipment_catalog.json": []byte(`[{"item_type": 20}]`),
		"stackable_catalog.json": []byte(`[{"id": 1}]`),
		"map_catalog.json":       []byte(`[{"normal_eligible":true,"store_eligible":true}]`),
		pvfSkillStateExportName:  []byte(`[{"job": 1}]`),
		pvfLevelExpExportName:    []byte(`[0,0,1000]`),
		pvfItemInfoExportName:    []byte("iteminfo"),
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

func TestPVFExportsCurrentRequiresSourceMD5(t *testing.T) {
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
	if pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("metadata-only source identity was treated as current")
	}

	got.MD5 = ""
	if err := WriteJSON(manifestPath, got); err != nil {
		t.Fatal(err)
	}
	if pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("manifest without stored md5 was treated as current")
	}
}

func TestEnsurePVFItemInfoDATReusesCurrentExport(t *testing.T) {
	dir := t.TempDir()
	pvfPath := filepath.Join(dir, "Script.pvf")
	if err := os.WriteFile(pvfPath, []byte("not a real pvf"), 0644); err != nil {
		t.Fatal(err)
	}
	writeCurrentPVFExportFiles(t, dir)
	stat, err := os.Stat(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(dir, "pvf_manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	path, err := EnsurePVFItemInfoDAT(pvfPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, pvfItemInfoExportName) {
		t.Fatalf("path=%q", path)
	}
}

func TestPublishPVFDirectoryReplacesWholeSnapshot(t *testing.T) {
	root := t.TempDir()
	pvfDir := filepath.Join(root, "pvf")
	tempDir := filepath.Join(root, "tmp")
	stageDir := filepath.Join(tempDir, "stage")
	if err := os.MkdirAll(pvfDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pvfDir, "old.json"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "new.json"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := publishPVFDirectory(stageDir, pvfDir, tempDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pvfDir, "old.json")); !os.IsNotExist(err) {
		t.Fatalf("old snapshot survived publish: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(pvfDir, "new.json")); err != nil || string(data) != "new" {
		t.Fatalf("published snapshot=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "pvf.previous")); !os.IsNotExist(err) {
		t.Fatalf("previous snapshot was not cleaned: %v", err)
	}
}

func TestRecoverPVFPublishRestoresInterruptedSnapshot(t *testing.T) {
	root := t.TempDir()
	pvfDir := filepath.Join(root, "pvf")
	tempDir := filepath.Join(root, "tmp")
	backupDir := filepath.Join(tempDir, "pvf.previous")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), []byte("previous"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := recoverPVFPublish(pvfDir, tempDir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(pvfDir, "manifest.json")); err != nil || string(data) != "previous" {
		t.Fatalf("restored snapshot=%q err=%v", data, err)
	}
}

func TestRecoverPVFPublishRemovesInterruptedStagingDirectories(t *testing.T) {
	root := t.TempDir()
	pvfDir := filepath.Join(root, "pvf")
	tempDir := filepath.Join(root, "tmp")
	stageDir := filepath.Join(tempDir, "pvf-export-stale")
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "partial.json"), []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := recoverPVFPublish(pvfDir, tempDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stageDir); !os.IsNotExist(err) {
		t.Fatalf("interrupted staging directory was not removed: %v", err)
	}
}

func TestPVFExportsCurrentRejectsSameMetadataWithDifferentContent(t *testing.T) {
	dir := t.TempDir()
	pvfPath := filepath.Join(dir, "Script.pvf")
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.WriteFile(pvfPath, []byte("first-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pvfPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		t.Fatal(err)
	}
	writeCurrentPVFExportFiles(t, dir)
	if err := WriteJSON(filepath.Join(dir, pvfManifestName), manifest); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(pvfPath, []byte("other-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pvfPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	stat, err = os.Stat(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		t.Fatal(err)
	}
	if pvfExportsCurrent(filepath.Join(dir, pvfManifestName), changed, dir) {
		t.Fatal("same-size same-time replacement reused stale PVF exports")
	}
}

func TestPVFExportsCurrentInvalidatesOldMapEligibilitySchema(t *testing.T) {
	dir := t.TempDir()
	writeCurrentPVFExportFiles(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "map_catalog.json"), []byte(`[{"village":1,"area":0,"use":true}]`), 0644); err != nil {
		t.Fatal(err)
	}
	want := pvfManifest{
		Version: pvfExportVersion, SkillStateVersion: pvfSkillStateExportVersion,
		Source: "/game/Script.pvf", Size: 100, ModTime: 200, MD5: "abc",
	}
	manifestPath := filepath.Join(dir, "pvf_manifest.json")
	if err := WriteJSON(manifestPath, want); err != nil {
		t.Fatal(err)
	}
	if pvfExportsCurrent(manifestPath, want, dir) {
		t.Fatal("map catalog without dynamic eligibility was treated as current")
	}
}

func TestPVFExportMarkersCurrentHandlesChunkBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	marker := []byte(`"item_type": 20`)
	data := append(bytes.Repeat([]byte{'x'}, pvfMarkerScanChunk-len(marker)+3), marker...)
	data = append(data, bytes.Repeat([]byte{' '}, 32)...)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	scratch := make([]byte, pvfMarkerScanChunk+pvfMarkerScanOverlap)
	if !pvfExportMarkersCurrent(path, pvfEquipmentMarkers, scratch) {
		t.Fatal("marker spanning read chunks was not detected")
	}
	if err := os.WriteFile(path, append(data, []byte(`"source_path"`)...), 0644); err != nil {
		t.Fatal(err)
	}
	if pvfExportMarkersCurrent(path, pvfEquipmentMarkers, scratch) {
		t.Fatal("legacy source marker was not rejected")
	}
}

func BenchmarkPVFExportsCurrentStreaming(b *testing.B) {
	dir := b.TempDir()
	large := bytes.Repeat([]byte{' '}, 2*1024*1024)
	files := map[string][]byte{
		pvfEquipmentExportName:  append([]byte(`{"item_type": 20}`), large...),
		pvfStackableExportName:  append([]byte(`[{"id":1}]`), large...),
		pvfMapExportName:        append([]byte(`{"normal_eligible":true,"store_eligible":true}`), large...),
		pvfSkillStateExportName: []byte(`[{"job":1}]`),
		pvfLevelExpExportName:   []byte(`[0,0,1000]`),
		pvfItemInfoExportName:   []byte("iteminfo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			b.Fatal(err)
		}
	}
	want := pvfManifest{Version: pvfExportVersion, SkillStateVersion: pvfSkillStateExportVersion, Source: "/game/Script.pvf", Size: 100, ModTime: 200, MD5: "abc"}
	if err := WriteJSON(filepath.Join(dir, pvfManifestName), want); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(6 * 1024 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !pvfExportsCurrent(filepath.Join(dir, pvfManifestName), want, dir) {
			b.Fatal("exports unexpectedly invalid")
		}
	}
}

func writeCurrentPVFExportFiles(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{
		"equipment_catalog.json": []byte(`[{"item_type": 20}]`),
		"stackable_catalog.json": []byte(`[{"id": 1}]`),
		"map_catalog.json":       []byte(`[{"normal_eligible":true,"store_eligible":true}]`),
		pvfSkillStateExportName:  []byte(`[{"job": 1, "skill_index": 1, "state": 1}]`),
		pvfLevelExpExportName:    []byte(`[0,0,1000]`),
		pvfItemInfoExportName:    []byte("iteminfo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
