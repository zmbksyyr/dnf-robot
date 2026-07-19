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
		if len(got) != 1 || got[0].SkillIndex != 3 || got[0].State != 22 || len(got[0].StateData) != 3 || got[0].StateData[0] != 3 {
			t.Fatalf("loaded skill snapshot for %q = %+v", dfGameR, got)
		}
	}
}
