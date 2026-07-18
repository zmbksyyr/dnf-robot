package pvf

import (
	"reflect"
	"testing"
)

func TestExtractSkillStateCatalogKeepsOnlyEmptyDataStates(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_atmage_load_state.nut": {
			Name: "sqr/character/new_atmage_load_state.nut",
			Data: []byte(`
IRDSQRCharacter.pushScriptFiles("character/atmage/header.nut");
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_AT_MAGE, "character/atmage/safe/safe.nut", "safe", STATE_SAFE, SKILL_SAFE);
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_AT_MAGE, "character/atmage/data/data.nut", "data", 22, 3);
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_AT_MAGE, "character/atmage/native/native.nut", "native", 23, 4);
`),
		},
		"sqr/character/atmage/header.nut": {
			Name: "sqr/character/atmage/header.nut",
			Data: []byte("STATE_SAFE <- 20;\nSKILL_SAFE <- 1;\n"),
		},
		"sqr/character/atmage/safe/safe.nut": {
			Name: "sqr/character/atmage/safe/safe.nut",
			Data: []byte("function checkExecutableSkill_safe(obj) { obj.sq_AddSetStatePacket(STATE_SAFE, 0, false); }\nfunction onAfterSetState_safe(obj, state, datas, reset) { obj.setCurrentAnimation(1); }"),
		},
		"sqr/character/atmage/data/data.nut": {
			Name: "sqr/character/atmage/data/data.nut",
			Data: []byte("function onAfterSetState_data(obj, state, datas, reset) { return obj.sq_GetVectorData(datas, 0); }"),
		},
		"sqr/character/atmage/native/native.nut": {
			Name: "sqr/character/atmage/native/native.nut",
			Data: []byte("function onSetState_native(obj, state, datas, reset) { SetState_native(obj, state, datas, reset); }\nfunction SetState_native(obj, state, values, reset) { obj.setCurrentAnimation(2); }"),
		},
	}}

	got := extractSkillStateCatalog(archive)
	if len(got) != 2 {
		t.Fatalf("catalog size = %d, want 2: %+v", len(got), got)
	}
	want := SkillState{Job: 8, SkillIndex: 1, State: 20, ScriptPath: "sqr/character/atmage/safe/safe.nut"}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("catalog entry = %+v, want %+v", got[0], want)
	}
	want = SkillState{Job: 8, SkillIndex: 4, State: 23, ScriptPath: "sqr/character/atmage/native/native.nut"}
	if !reflect.DeepEqual(got[1], want) {
		t.Fatalf("catalog entry = %+v, want %+v", got[1], want)
	}
}

func TestExtractSkillStateCatalogIncludesVerifiedShiningCutData(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_thief_load_state.nut": {
			Name: "sqr/character/new_thief_load_state.nut",
			Data: []byte(`IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_THIEF, "character/thief/1_rogue/shiningcut/shiningcut.nut", "thief_shiningcut", 22, 3);`),
		},
		"sqr/character/thief/1_rogue/shiningcut/shiningcut.nut": {
			Name: "sqr/character/thief/1_rogue/shiningcut/shiningcut.nut",
			Data: []byte("function onAfterSetState_thief_shiningcut(obj, state, datas, reset) { return obj.sq_GetVectorData(datas, 0); }"),
		},
	}}

	got := extractSkillStateCatalog(archive)
	want := []SkillState{{
		Job: 6, SkillIndex: 3, State: 22,
		ScriptPath: "sqr/character/thief/1_rogue/shiningcut/shiningcut.nut",
		StateData:  []byte{0x03, 0x00, 0x00}, Verified: true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verified skill catalog = %+v, want %+v", got, want)
	}
}

func TestExtractSkillStateCatalogRejectsScriptsWithoutStateHandler(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_swordman_load_state.nut": {
			Name: "sqr/character/new_swordman_load_state.nut",
			Data: []byte(`
IRDSQRCharacter.pushState(0, "character/swordman/passive.nut", "passive", 13, 18);
IRDSQRCharacter.pushState(0, "character/swordman/reactive.nut", "reactive", 32, 58);
`),
		},
		"sqr/character/swordman/passive.nut": {
			Name: "sqr/character/swordman/passive.nut",
			Data: []byte("function checkExecutableSkill_passive(obj) { obj.appendage(); }"),
		},
		"sqr/character/swordman/reactive.nut": {
			Name: "sqr/character/swordman/reactive.nut",
			Data: []byte("function onAttack_reactive(obj) { obj.sq_AddSetStatePacket(32, 0, false); }"),
		},
	}}

	if got := extractSkillStateCatalog(archive); len(got) != 0 {
		t.Fatalf("unsafe skills entered catalog: %+v", got)
	}
}

func TestSkillStatesForJobReturnsCopy(t *testing.T) {
	setSkillStateCatalog([]SkillState{{Job: 1, SkillIndex: 2, State: 3, StateData: []byte{1, 2}}, {Job: 2, SkillIndex: 4, State: 5}})
	got := SkillStatesForJob(1)
	if len(got) != 1 || got[0].SkillIndex != 2 {
		t.Fatalf("job catalog = %+v", got)
	}
	got[0].SkillIndex = 99
	got[0].StateData[0] = 99
	again := SkillStatesForJob(1)
	if len(again) != 1 || again[0].SkillIndex != 2 || !reflect.DeepEqual(again[0].StateData, []byte{1, 2}) {
		t.Fatalf("catalog mutated through returned slice: %+v", again)
	}
}
