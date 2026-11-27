package shared

import "testing"

func materialDefinitionTestValue() SkillMaterialDefinition {
	return SkillMaterialDefinition{Known: true, MaximumLevel: 70, Blocks: []SkillMaterialBlock{
		{Mode: "common", Rows: []SkillMaterialRow{{ItemID: 3037, Start: 1, End: 2}}},
		{Mode: "pvp", Rows: []SkillMaterialRow{}},
	}}
}

func TestSkillMaterialDefinitionSnapshotsOwnTheirNestedRows(t *testing.T) {
	t.Cleanup(func() { SetSkillStates(nil); SetPartySkillStates(nil) })
	value := materialDefinitionTestValue()
	SetSkillStates([]SkillState{{Job: 0, SkillIndex: 9, State: 23, Materials: value}})
	SetPartySkillStates([]PartySkillState{{Job: 0, SkillIndex: 9, State: 23, Materials: value}})
	value.Blocks[0].Rows[0].Start = 999
	value.Blocks[1].Mode = "dungeon"
	for i := 0; i < 2; i++ {
		a := SkillStatesForJob(0)
		b := PartySkillStatesForJob(0)
		if !a[0].Materials.Equal(materialDefinitionTestValue()) || !b[0].Materials.Equal(materialDefinitionTestValue()) {
			t.Fatal("caller changed published material snapshot")
		}
		a[0].Materials.Blocks[0].Rows[0].End = 888
		b[0].Materials.Blocks[0].Rows[0].End = 777
	}
}

func TestSkillMaterialMatchingRejectsConflictsAndIgnoresWhitelistCosts(t *testing.T) {
	definition := materialDefinitionTestValue()
	for _, path := range []string{"", "skill/a.nut"} {
		for _, conflict := range []bool{false, true} {
			pvf := []SkillState{{Job: 0, SkillIndex: 9, State: 23, ScriptPath: "skill/a.nut", Materials: definition}}
			other := pvf[0]
			other.Materials = definition.Clone()
			if path == "" {
				other.ScriptPath = "skill/b.nut"
			}
			if conflict {
				other.Materials.Blocks[0].Rows[0].End++
			}
			pvf = append(pvf, other)
			for _, reversed := range []bool{false, true} {
				if reversed {
					pvf[0], pvf[1] = pvf[1], pvf[0]
				}
				matches, _ := MatchPartySkillStates(0, []PartySkillState{{Job: 0, SkillIndex: 9, State: 23,
					ScriptPath: path, Materials: SkillMaterialDefinition{Known: true, MaximumLevel: 1}}}, pvf)
				if len(matches) != 1 || matches[0].Materials.Known == conflict {
					t.Fatal("material conflict or whitelist replaced PVF definition", matches)
				}
				if !conflict && !matches[0].Materials.Equal(definition) {
					t.Fatal("matching material definition lost", matches[0].Materials)
				}
				if len(matches[0].Materials.Blocks) > 0 {
					matches[0].Materials.Blocks[0].Rows[0].Start = 888
					if !pvf[0].Materials.Equal(definition) {
						t.Fatal("matching result mutated source material rows")
					}
				}
			}
		}
	}
}
