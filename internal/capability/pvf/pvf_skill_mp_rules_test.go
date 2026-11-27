package pvf

import (
	"encoding/json"
	"testing"

	"robot/internal/shared"
)

func TestSkillMPRulesMatchNativeDefinitionFields(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       shared.SkillMPRules
	}{
		{"missing-definition", "", shared.SkillMPRules{}},
		{"no-weapon-effect", "[type]\n`[active]`", shared.SkillMPRules{Known: true}},
		{"physical-upper-slash", "[skill class]\n1\n[weapon effect type]\n`[physical]`", shared.SkillMPRules{Known: true, WeaponEffect: 1}},
		{"magical-class-independent", "[skill class]\n1\n[weapon effect type]\n`[magical]`", shared.SkillMPRules{Known: true, WeaponEffect: 2}},
		{"command-mp-is-second", "[weapon effect type]\n`[physical]`\n[skill command advantage]\n10 20", shared.SkillMPRules{Known: true, WeaponEffect: 1, CommandReductionPermille: 20}},
		{"decoded-token-whitespace", "[weapon effect type]\r\n`[magical]`\t\n[skill command advantage]\r\n10\t20\t\r\n[dungeon]", shared.SkillMPRules{Known: true, WeaponEffect: 2, CommandReductionPermille: 20}},
		{"unknown-effect", "[weapon effect type]\n`[other]`", shared.SkillMPRules{}},
		{"empty-effect", "[weapon effect type]\n[type]\n`[active]`", shared.SkillMPRules{}},
		{"truncated-command", "[skill command advantage]\n10", shared.SkillMPRules{}},
		{"negative-command", "[skill command advantage]\n10 -20", shared.SkillMPRules{}},
		{"invalid-first-command", "[skill command advantage]\nbad 20", shared.SkillMPRules{}},
		{"overflow-command", "[skill command advantage]\n10 2147483648", shared.SkillMPRules{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSkillMPRules(tc.text); got != tc.want {
				t.Fatalf("MP rules = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSkillCatalogRetainsMPDefinitionWithoutAssumingEffectiveCost(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"sqr/example_load_state.nut":    {Data: []byte(`IRDSQRCharacter.pushState(8,"example.nut","example",20,1);`)},
		"sqr/example.nut":               {Data: []byte("function onSetState_example(obj,state,data,reset) {}")},
		"skill/atmageskill.lst":         {Data: []byte("1 `atmage/example.skl`")},
		"skill/atmage/example.skl":      {Data: []byte("[weapon effect type]\n`[magical]`\n[skill command advantage]\n10 20\n[consume MP]\n100 100")},
		"skill/swordmanskill.lst":       {Data: []byte("1 `swordman/example.skl`\n46 `swordman/upperslash.skl`")},
		"skill/swordman/example.skl":    {Data: []byte("[weapon effect type]\n`[physical]`\n[consume MP]\n1 1")},
		"skill/swordman/upperslash.skl": {Data: []byte("[type]\n`[active]`\n[weapon effect type]\n`[physical]`\n[consume MP]\n1 1")},
	}}
	entries := extractSkillStateCatalog(a)
	if len(entries) != 2 || entries[0].Job != 0 || entries[0].MPRules.WeaponEffect != 1 || entries[1].Job != 8 {
		t.Fatalf("job or native state lost: %+v", entries)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []SkillState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	want := shared.SkillMPRules{Known: true, WeaponEffect: 2, CommandReductionPermille: 20}
	if decoded[1].MPRules != want || len(decoded[1].BaseMPByLevel) != 1 || decoded[1].BaseMPByLevel[0] != 100 {
		t.Fatalf("MP rules or base MP lost in catalog: %+v", decoded[1])
	}
	var old SkillState
	if err := json.Unmarshal([]byte(`{"job":8,"skill_index":1,"base_mp_by_level":[100]}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.MPRules.Known {
		t.Fatal("old catalog invented known MP rules")
	}
}
