package pvf

import "testing"

func TestExtractSkillStateCatalogKeepsOnlyEmptyDataStates(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_atmage_load_state.nut": {
			Name: "sqr/character/new_atmage_load_state.nut",
			Data: []byte(`
IRDSQRCharacter.pushScriptFiles("character/atmage/header.nut");
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_AT_MAGE, "character/atmage/safe/safe.nut", "safe", STATE_SAFE, SKILL_SAFE);
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_AT_MAGE, "character/atmage/data/data.nut", "data", 22, 3);
`),
		},
		"sqr/character/atmage/header.nut": {
			Name: "sqr/character/atmage/header.nut",
			Data: []byte("STATE_SAFE <- 20;\nSKILL_SAFE <- 1;\n"),
		},
		"sqr/character/atmage/safe/safe.nut": {
			Name: "sqr/character/atmage/safe/safe.nut",
			Data: []byte("function onAfterSetState_safe(obj, state, datas, reset) { obj.setCurrentAnimation(1); }"),
		},
		"sqr/character/atmage/data/data.nut": {
			Name: "sqr/character/atmage/data/data.nut",
			Data: []byte("function onAfterSetState_data(obj, state, datas, reset) { return obj.sq_GetVectorData(datas, 0); }"),
		},
	}}

	got := extractSkillStateCatalog(archive)
	if len(got) != 1 {
		t.Fatalf("catalog size = %d, want 1: %+v", len(got), got)
	}
	want := SkillState{Job: 8, SkillIndex: 1, State: 20, ScriptPath: "sqr/character/atmage/safe/safe.nut"}
	if got[0] != want {
		t.Fatalf("catalog entry = %+v, want %+v", got[0], want)
	}
}

func TestSkillStatesForJobReturnsCopy(t *testing.T) {
	setSkillStateCatalog([]SkillState{{Job: 1, SkillIndex: 2, State: 3}, {Job: 2, SkillIndex: 4, State: 5}})
	got := SkillStatesForJob(1)
	if len(got) != 1 || got[0].SkillIndex != 2 {
		t.Fatalf("job catalog = %+v", got)
	}
	got[0].SkillIndex = 99
	again := SkillStatesForJob(1)
	if len(again) != 1 || again[0].SkillIndex != 2 {
		t.Fatalf("catalog mutated through returned slice: %+v", again)
	}
}
