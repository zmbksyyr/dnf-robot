package pvf

import (
	"reflect"
	"testing"
)

func TestDungeonSkillCooldownUsesPublicDefaultAndDungeonOverride(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         []int
	}{
		{"public-only", "[cool time]\n8000\t8000\n", []int{8000}},
		{"pvp-override-isolated", "[cool time]\n8000 8000\n[pvp]\n[cool time]\n100 100\n[/pvp]", []int{8000}},
		{"dungeon-override", "[cool time]\n8000 8000\n[dungeon]\n[cool time]\n15000 15000\n[/dungeon]", []int{15000}},
		{"pvp-before-public", "[pvp]\n[cool time]\n100 100\n[/pvp]\n[cool time]\n8000 8000\n", []int{8000}},
		{"dungeon-inherits", "[cool time]\n8000 8000\n[dungeon]\n[consume MP]\n10 20\n[/dungeon]", []int{8000}},
		{"variable-override", "[maximum level]\n3\n[cool time]\n8000 8000\n[dungeon]\n[cool time]\n500 1500\n[/dungeon]", []int{500, 1000, 1500}},
		{"pvp-only", "[pvp]\n[cool time]\n100 100\n[/pvp]", nil},
		{"variable-without-level-count", "[cool time]\n8000 9000\n", nil},
		{"other-modes-isolated", "[death tower]\n[cool time]\n100 100\n[/death tower]\n[warroom]\n[cool time]\n200 200\n[/warroom]", nil},
		{"decreasing-to-zero", "[maximum level]\n3\n[cool time]\n1000 0\n", []int{1000, 500, 0}},
		{"single-level-endpoint", "[maximum level]\n1\n[cool time]\n1000 500\n", []int{500}},
		{"open-ended-dungeon-teleport", "[maximum level]\n1\n[dungeon]\n[cool time]\n1500 1500\n[pvp]\n[cool time]\n10000 10000\n[/pvp]", []int{1500}},
		{"open-ended-inherits-with-pvp-isolated", "[cool time]\n8000 8000\n[dungeon]\n[consume MP]\n40 56\n[pvp]\n[cool time]\n100 100\n[/pvp]", []int{8000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dungeonSkillCooldowns(tc.script); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("cooldown=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSkillCooldownUsesListedDungeonSkillAndRejectsUnknownTables(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":   {Data: []byte("9 `swordman/slash.skl`\n10 `swordman/scaled.skl`\n11 `swordman/zero.skl`\n12 `swordman/missing.skl`")},
		"skill/swordman/slash.skl":  {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n15000\t15000\n[/dungeon]\n[pvp]\n[cool time]\n18000 18000\n[/pvp]\n// eof")},
		"skill/swordman/scaled.skl": {Data: []byte("#PVF_File\n[maximum level]\n3\n[dungeon]\n[cool time]\n1000 2000\n[/dungeon]\n// eof")},
		"skill/swordman/zero.skl":   {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n-1 1000\n[/dungeon]\n// eof")},
		"skill/atmageskill.lst":     {Data: []byte("9 `atmage/light.skl`\n3 `atmage/crystal.skl`")},
		"skill/atmage/light.skl":    {Data: []byte("#PVF_File\n[pvp]\n[cool time]\n100 100\n[/pvp]\n[dungeon]\n[cool time]\n20000\n20000\n[/dungeon]\n// eof")},
		"skill/atmage/crystal.skl":  {Data: []byte("#PVF_File\n[cool time]\n8000 8000\n[consume MP]\n10 10\n[pvp]\n[cool time]\n5000 5000\n[/pvp]\n// eof")},
	}}
	got := extractSkillCooldowns(a)
	want := map[[2]int][]int{{0, 9}: {15000}, {0, 10}: {1000, 1500, 2000}, {8, 9}: {20000}, {8, 3}: {8000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cooldowns mixed jobs, modes or unknown data: %v", got)
	}
	if len(extractSkillCooldowns(&pvfArchive{files: map[string]*pvfFile{}})) != 0 {
		t.Fatal("invented a cooldown without a PVF skill definition")
	}
}

func TestDungeonVariableCooldownMatchesOriginalClientTable(t *testing.T) {
	// Male mage skill 175, all 20 entries read from the original client.
	// See doc/windows.md for the baseline and external observation receipt.
	want := []int{25000, 29210, 33421, 37631, 41842, 46052, 50263, 54473, 58684, 62894,
		67105, 71315, 75526, 79736, 83947, 88157, 92368, 96578, 100789, 105000}
	got := dungeonSkillCooldowns("[maximum level]\n20\n[cool time]\n25000 105000\n[pvp]\n[cool time]\n15000 30000\n[/pvp]")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("original client cooldowns differ: %v", got)
	}
}

func TestExtractedSkillStateCarriesPVFCooldown(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"sqr/example_load_state.nut": {Data: []byte(`IRDSQRCharacter.pushState(0,"example.nut","example",249,9);`)},
		"sqr/example.nut":            {Data: []byte("function onSetState_example(obj,state,data,reset) {}")},
		"skill/swordmanskill.lst":    {Data: []byte("9 `swordman/example.skl`")},
		"skill/swordman/example.skl": {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n15000 15000\n[/dungeon]\n// eof")},
	}}
	got := extractSkillStateCatalog(a)
	if len(got) != 1 || got[0].CooldownMS != 15000 {
		t.Fatalf("extracted state lost its actual skill cooldown: %+v", got)
	}
	a.files["skill/swordman/example.skl"].Data = []byte("#PVF_File\n[maximum level]\n3\n[cool time]\n1000 0\n")
	got = extractSkillStateCatalog(a)
	if len(got) != 1 || got[0].CooldownMS != 0 || !reflect.DeepEqual(got[0].CooldownByLevelMS, []int{1000, 500, 0}) {
		t.Fatalf("state export lost its variable cooldown: %+v", got)
	}
}
