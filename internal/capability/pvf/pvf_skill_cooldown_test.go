package pvf

import "testing"

func TestSkillCooldownUsesListedDungeonSkillAndRejectsUnknownTables(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":   {Data: []byte("9 `swordman/slash.skl`\n10 `swordman/scaled.skl`\n11 `swordman/zero.skl`\n12 `swordman/missing.skl`")},
		"skill/swordman/slash.skl":  {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n15000\t15000\n[/dungeon]\n[pvp]\n[cool time]\n18000 18000\n[/pvp]")},
		"skill/swordman/scaled.skl": {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n1000 2000\n[/dungeon]\n// eof")},
		"skill/swordman/zero.skl":   {Data: []byte("#PVF_File\n[dungeon]\n[cool time]\n0 1000\n[/dungeon]\n// eof")},
		"skill/atmageskill.lst":     {Data: []byte("9 `atmage/light.skl`")},
		"skill/atmage/light.skl":    {Data: []byte("#PVF_File\n[pvp]\n[cool time]\n100 100\n[/pvp]\n[dungeon]\n[cool time]\n20000\n20000\n[/dungeon]\n// eof")},
	}}
	got := extractSkillCooldowns(a)
	if len(got) != 2 || got[[2]int{0, 9}] != 15000 || got[[2]int{8, 9}] != 20000 {
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
