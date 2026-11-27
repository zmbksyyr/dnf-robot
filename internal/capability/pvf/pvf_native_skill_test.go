package pvf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"robot/internal/capability/catalog"
	"robot/internal/shared"
)

func TestExtractNativeUpperSlashRequiresCurrentActiveDefinition(t *testing.T) {
	const definition = "[type]\n`[active]`\n[dungeon]\n[cool time]\n2000 2000\n[consume MP]\n1 1\n[/dungeon]"
	for _, tc := range []struct {
		name, list, path, text string
		want                   bool
	}{
		{"current", "46 `Swordman/UpperSlash.skl`", "skill/swordman/upperslash.skl", definition, true},
		{"missing definition", "46 `Swordman/UpperSlash.skl`", "", "", false},
		{"unlisted", "", "skill/swordman/upperslash.skl", definition, false},
		{"wrong skill", "45 `Swordman/UpperSlash.skl`", "skill/swordman/upperslash.skl", definition, false},
		{"replaced skill", "46 `Swordman/Other.skl`", "skill/swordman/other.skl", definition, false},
		{"passive", "46 `Swordman/UpperSlash.skl`", "skill/swordman/upperslash.skl", "[type]\n`[passive]`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &pvfArchive{files: map[string]*pvfFile{
				"skill/swordmanskill.lst": {Data: []byte(tc.list)},
			}}
			if tc.path != "" {
				a.files[tc.path] = &pvfFile{Data: []byte(tc.text)}
			}
			got := extractSkillStateCatalog(a)
			if !tc.want {
				if len(got) != 0 {
					t.Fatalf("unsupported native skill exported: %+v", got)
				}
				return
			}
			want := []SkillState{{Job: 0, SkillIndex: 46, State: 28, ScriptPath: tc.path,
				StateData: []int{2, 46}, CooldownMS: 2000, BaseMPByLevel: []int{1}, MPRules: shared.SkillMPRules{Known: true}, ItemCostFree: true}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("native metadata = %+v, want %+v", got, want)
			}
		})
	}
}

func TestNativeUpperSlashBecomesAutoCandidateInExistingCatalog(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":       {Data: []byte("46 `Swordman/UpperSlash.skl`")},
		"skill/swordman/upperslash.skl": {Data: []byte("[type]\n`[active]`\n[cool time]\n2000 2000\n")},
	}}
	states := extractSkillStateCatalog(a)
	dir := t.TempDir()
	pvfPath, catalogPath := filepath.Join(dir, "pvf.json"), filepath.Join(dir, "catalog.json")
	if err := WriteJSON(pvfPath, states); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SyncPartySkillCatalog(catalogPath, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ReadPartySkillCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) > 0 {
		t.Fatalf("issues=%+v", report.Issues)
	}
	matches, stats := shared.MatchPartySkillStatesMode(true, 0, report.Entries, states)
	if len(matches) != 1 || stats.PVFMatched != 1 || matches[0].SkillIndex != 46 || matches[0].State != 28 || !reflect.DeepEqual(matches[0].StateData, []byte{2, 46}) || matches[0].CooldownMS != 2000 {
		t.Fatalf("native auto candidate = %+v stats=%+v", matches, stats)
	}
	// The parameters published in a shared snapshot must not borrow the
	// caller's slices: one editor must not change another robot's next cast.
	previous := shared.SkillStatesForJob(0)
	t.Cleanup(func() { shared.SetSkillStates(previous) })
	shared.SetSkillStates(states)
	states[0].StateData[0] = 99
	copy := shared.SkillStatesForJob(0)
	copy[0].StateData[0] = 88
	if shared.SkillStatesForJob(0)[0].StateData[0] != 2 {
		t.Fatal("native parameter snapshot aliased")
	}
}

func TestCapturedGhostSlashEntersAutomaticCatalog(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":       {Data: []byte("5 `Swordman/HardAttack.skl`")},
		"skill/swordman/hardattack.skl": {Data: []byte("[type]\n`[active]`\n[cool time]\n6000 6000\n[consume MP]\n5 5")},
	}}
	states := extractSkillStateCatalog(a)
	dir := t.TempDir()
	pvfPath, catalogPath := filepath.Join(dir, "pvf.json"), filepath.Join(dir, "catalog.json")
	if err := WriteJSON(pvfPath, states); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SyncPartySkillCatalog(catalogPath, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ReadPartySkillCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := shared.MatchPartySkillStatesMode(true, 0, report.Entries, states)
	if len(matches) != 1 || matches[0].SkillIndex != 5 || matches[0].State != 20 || !reflect.DeepEqual(matches[0].StateData, []byte{0, 0}) || matches[0].CooldownMS != 6000 {
		t.Fatalf("captured Ghost Slash did not reach automatic selection: %+v", matches)
	}
}

func TestExtractNativeMageUpperSwingRequiresCurrentActiveDefinition(t *testing.T) {
	for _, tc := range []struct {
		name, list, text string
		want             bool
	}{
		{"current", "12 `Mage/UpperSwing.skl`", "[type]\n`[active]`\n[cool time]\n2000 2000\n[consume MP]\n1 1\n[weapon effect type]\n`[physical]`", true},
		{"missing", "12 `Mage/UpperSwing.skl`", "", false},
		{"unlisted", "", "[type]\n`[active]`", false},
		{"different id", "11 `Mage/UpperSwing.skl`", "[type]\n`[active]`", false},
		{"different definition", "12 `Mage/Other.skl`", "[type]\n`[active]`", false},
		{"passive", "12 `Mage/UpperSwing.skl`", "[type]\n`[passive]`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &pvfArchive{files: map[string]*pvfFile{
				"skill/mageskill.lst":       {Data: []byte(tc.list)},
				"skill/mage/upperswing.skl": {Data: []byte(tc.text)},
			}}
			got := extractSkillStateCatalog(a)
			if !tc.want {
				if len(got) != 0 {
					t.Fatalf("unsupported mage skill: %+v", got)
				}
				return
			}
			want := []SkillState{{Job: 3, SkillIndex: 12, State: 23, ScriptPath: "skill/mage/upperswing.skl", StateData: []int{12}, CooldownMS: 2000, BaseMPByLevel: []int{1}, MPRules: shared.SkillMPRules{Known: true, WeaponEffect: 1}, ItemCostFree: true}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mage metadata = %+v, want %+v", got, want)
			}
		})
	}
}

func TestNativeMageUpperSwingJoinsDefaultAutoCatalog(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/mageskill.lst":       {Data: []byte("12 `Mage/UpperSwing.skl`")},
		"skill/mage/upperswing.skl": {Data: []byte("[type]\n`[active]`\n[cool time]\n2000 2000\n[consume MP]\n1 1")},
	}}
	states := extractSkillStateCatalog(a)
	dir := t.TempDir()
	catalogPath, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
	if err := os.WriteFile(catalogPath, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(pvfPath, states); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SyncPartySkillCatalog(catalogPath, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ReadPartySkillCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	got, stats := shared.MatchPartySkillStatesMode(report.Auto(), 3, report.Entries, states)
	if len(report.Issues) != 0 || len(got) != 1 || stats.PVFMatched != 1 || got[0].SkillIndex != 12 || got[0].State != 23 || !reflect.DeepEqual(got[0].StateData, []byte{12}) || got[0].CooldownMS != 2000 || !got[0].ItemCostFree {
		t.Fatalf("mage not automatically matched: %+v %+v %+v", got, stats, report.Issues)
	}
}

func TestNativeGunnerAerialKneeUsesCurrentDefinitionAndDefaultAutoCatalog(t *testing.T) {
	const definition = "[type]\n`[active]`\n[cool time]\n2000 2000\n[consume MP]\n1 1\n[weapon effect type]\n`[physical]`"
	for _, tc := range []struct {
		name, list, text string
		want             bool
	}{
		{"current", "4 `Gunner/AerialKnee.skl`", definition, true},
		{"missing", "4 `Gunner/AerialKnee.skl`", "", false},
		{"unlisted", "", definition, false},
		{"different id", "3 `Gunner/AerialKnee.skl`", definition, false},
		{"different path", "4 `Gunner/Other.skl`", definition, false},
		{"passive", "4 `Gunner/AerialKnee.skl`", "[type]\n`[passive]`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &pvfArchive{files: map[string]*pvfFile{
				"skill/gunnerskill.lst":       {Data: []byte(tc.list)},
				"skill/gunner/aerialknee.skl": {Data: []byte(tc.text)},
			}}
			states := extractSkillStateCatalog(a)
			if !tc.want {
				if len(states) != 0 {
					t.Fatalf("unsupported gunner definition exported: %+v", states)
				}
				return
			}
			dir := t.TempDir()
			catalogPath, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
			if err := os.WriteFile(catalogPath, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := WriteJSON(pvfPath, states); err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.SyncPartySkillCatalog(catalogPath, pvfPath); err != nil {
				t.Fatal(err)
			}
			report, err := catalog.ReadPartySkillCatalog(catalogPath)
			if err != nil {
				t.Fatal(err)
			}
			got, stats := shared.MatchPartySkillStatesMode(report.Auto(), 2, report.Entries, states)
			if len(report.Issues) != 0 || len(got) != 1 || stats.PVFMatched != 1 || got[0].SkillIndex != 4 || got[0].State != 20 || !reflect.DeepEqual(got[0].StateData, []byte{4}) || got[0].CooldownMS != 2000 || !reflect.DeepEqual(got[0].BaseMPByLevel, []int{1}) || !got[0].ItemCostFree || !got[0].MPRules.Known || got[0].MPRules.WeaponEffect != 1 {
				t.Fatalf("gunner not automatically matched: %+v %+v %+v", got, stats, report.Issues)
			}
		})
	}
}
