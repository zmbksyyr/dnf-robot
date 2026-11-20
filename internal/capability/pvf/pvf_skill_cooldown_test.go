package pvf

import "testing"

func TestDungeonSkillCooldownUsesPublicDefaultAndDungeonOverride(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         int
	}{
		{"public-only", "[cool time]\n8000\t8000\n", 8000},
		{"pvp-override-isolated", "[cool time]\n8000 8000\n[pvp]\n[cool time]\n100 100\n[/pvp]", 8000},
		{"dungeon-override", "[cool time]\n8000 8000\n[dungeon]\n[cool time]\n15000 15000\n[/dungeon]", 15000},
		{"pvp-before-public", "[pvp]\n[cool time]\n100 100\n[/pvp]\n[cool time]\n8000 8000\n", 8000},
		{"dungeon-inherits", "[cool time]\n8000 8000\n[dungeon]\n[consume MP]\n10 20\n[/dungeon]", 8000},
		{"variable-override-does-not-use-public", "[cool time]\n8000 8000\n[dungeon]\n[cool time]\n500 1500\n[/dungeon]", 0},
		{"pvp-only", "[pvp]\n[cool time]\n100 100\n[/pvp]", 0},
		{"public-variable", "[cool time]\n8000 9000\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dungeonSkillCooldown(tc.script); got != tc.want {
				t.Fatalf("cooldown=%d want %d", got, tc.want)
			}
		})
	}
}

func TestSkillCooldownUsesListedDungeonSkillAndRejectsUnknownTables(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":   {Data: []byte("9 `swordman/slash.skl`\n10 `swordman/scaled.skl`\n11 `swordman/zero.skl`\n12 `swordman/missing.skl`")},
		"skill/swordman/slash.skl":  {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n15000\t15000\n[/dungeon]\n[pvp]\n[cool time]\n18000 18000\n[/pvp]\n// eof")},
		"skill/swordman/scaled.skl": {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n1000 2000\n[/dungeon]\n// eof")},
		"skill/swordman/zero.skl":   {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n0 1000\n[/dungeon]\n// eof")},
		"skill/atmageskill.lst":     {Data: []byte("9 `atmage/light.skl`\n3 `atmage/crystal.skl`")},
		"skill/atmage/light.skl":    {Data: []byte("#PVF_File\n[pvp]\n[cool time]\n100 100\n[/pvp]\n[dungeon]\n[cool time]\n20000\n20000\n[/dungeon]\n// eof")},
		"skill/atmage/crystal.skl":  {Data: []byte("#PVF_File\n[cool time]\n8000 8000\n[consume MP]\n10 10\n[pvp]\n[cool time]\n5000 5000\n[/pvp]\n// eof")},
	}}
	got := extractSkillCooldowns(a)
	if len(got) != 3 || got[[2]int{0, 9}] != 15000 || got[[2]int{8, 9}] != 20000 || got[[2]int{8, 3}] != 8000 {
		t.Fatalf("cooldowns mixed jobs, modes or unknown data: %v", got)
	}
	if extractSkillCooldowns(&pvfArchive{files: map[string]*pvfFile{}})[[2]int{0, 9}] != 0 {
		t.Fatal("invented a cooldown without a PVF skill definition")
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
}
