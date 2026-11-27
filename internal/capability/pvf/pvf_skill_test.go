package pvf

import (
	"encoding/json"
	"reflect"
	"testing"

	"robot/internal/shared"
)

func TestSkillStateJSONPreservesKnownEmptyAndUnknownVectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []int
		want string
	}{
		{name: "known empty", data: []int{}, want: "[]"},
		{name: "unknown", data: nil, want: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(SkillState{Job: 8, SkillIndex: 1, State: 20, StateData: tc.data})
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			if got := string(document["native_state_data"]); got != tc.want {
				t.Fatalf("native_state_data = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExtractSkillStateCatalogIndexesResolvedPVFStates(t *testing.T) {
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
			Data: []byte("function checkExecutableSkill_safe(obj) { return true; }"),
		},
		"sqr/character/atmage/data/data.nut": {
			Name: "sqr/character/atmage/data/data.nut",
			Data: []byte("function onAfterSetState_data(obj, state, datas, reset) { return obj.sq_GetVectorData(datas, 0); }"),
		},
		"sqr/character/atmage/native/native.nut": {
			Name: "sqr/character/atmage/native/native.nut",
			Data: []byte("function onSetState_native(obj, state, datas, reset) { obj.setCurrentAnimation(2); }"),
		},
	}}

	want := []SkillState{
		{Job: 8, SkillIndex: 1, State: 20, ScriptPath: "sqr/character/atmage/safe/safe.nut"},
		{Job: 8, SkillIndex: 3, State: 22, ScriptPath: "sqr/character/atmage/data/data.nut"},
		{Job: 8, SkillIndex: 4, State: 23, ScriptPath: "sqr/character/atmage/native/native.nut"},
	}
	if got := extractSkillStateCatalog(archive); !reflect.DeepEqual(got, want) {
		t.Fatalf("skill state catalog = %+v, want %+v", got, want)
	}
}

func TestExtractSkillStateCatalogKeepsDistinctPathsForSameState(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_mage_load_state.nut": {
			Name: "sqr/character/new_mage_load_state.nut",
			Data: []byte(`
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_MAGE, "character/mage/a.nut", "a", 44, 7);
IRDSQRCharacter.pushState(ENUM_CHARACTERJOB_MAGE, "character/mage/b.nut", "b", 44, 7);
`),
		},
		"sqr/character/mage/a.nut": {Name: "sqr/character/mage/a.nut", Data: []byte("function a() { return true; }")},
		"sqr/character/mage/b.nut": {Name: "sqr/character/mage/b.nut", Data: []byte("function b() { return true; }")},
	}}

	want := []SkillState{
		{Job: 3, SkillIndex: 7, State: 44, ScriptPath: "sqr/character/mage/a.nut"},
		{Job: 3, SkillIndex: 7, State: 44, ScriptPath: "sqr/character/mage/b.nut"},
	}
	if got := extractSkillStateCatalog(archive); !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate state paths = %+v, want %+v", got, want)
	}
}

func TestExtractSkillStateCatalogRejectsMissingAndInvalidReferences(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/new_swordman_load_state.nut": {
			Name: "sqr/character/new_swordman_load_state.nut",
			Data: []byte(`
IRDSQRCharacter.pushState(0, "character/swordman/valid.nut", "valid", 13, 18);
IRDSQRCharacter.pushState(0, "character/swordman/missing.nut", "missing", 14, 19);
IRDSQRCharacter.pushState(0, "character/swordman/invalid.nut", "invalid", 300, 0);
`),
		},
		"sqr/character/swordman/valid.nut":   {Name: "sqr/character/swordman/valid.nut", Data: []byte("function valid() { return true; }")},
		"sqr/character/swordman/invalid.nut": {Name: "sqr/character/swordman/invalid.nut", Data: []byte("function invalid() { return true; }")},
	}}

	want := []SkillState{{Job: 0, SkillIndex: 18, State: 13, ScriptPath: "sqr/character/swordman/valid.nut"}}
	if got := extractSkillStateCatalog(archive); !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered skill states = %+v, want %+v", got, want)
	}
}

func TestSkillStatesForJobReturnsCopy(t *testing.T) {
	setSkillStateCatalog([]SkillState{{Job: 1, SkillIndex: 2, State: 3}, {Job: 2, SkillIndex: 4, State: 5}})
	got := shared.SkillStatesForJob(1)
	if len(got) != 1 || got[0].SkillIndex != 2 {
		t.Fatalf("job catalog = %+v", got)
	}
	got[0].SkillIndex = 99
	again := shared.SkillStatesForJob(1)
	if len(again) != 1 || again[0].SkillIndex != 2 {
		t.Fatalf("catalog mutated through returned slice: %+v", again)
	}
}

func TestExtractSkillStateCatalogIncludesCustomRegistrations(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"sqr/custom_pack/swordman_load_state.nut": {Data: []byte(`
IRDSQRCharacter.pushScriptFiles("custom_pack/header.nut");
IRDSQRCharacter.pushState(0, "custom_pack/slash.nut", "slash", STATE_SLASH, 9);
IRDSQRCharacter.pushState(0, "sqr/custom_pack/followup.nut", "followup", 249, 9);
IRDSQRCharacter.pushState(0, "custom_pack/slash.nut", "unmapped", 23, -1);
`)},
		"sqr/custom_pack/header.nut":   {Data: []byte("STATE_SLASH <- 23;")},
		"sqr/custom_pack/slash.nut":    {Data: []byte("function onSetState_slash(obj,state,data,reset) {}")},
		"sqr/custom_pack/followup.nut": {Data: []byte("function onSetState_followup(obj,state,data,reset) {}")},
	}}
	want := []SkillState{
		{Job: 0, SkillIndex: 9, State: 23, ScriptPath: "sqr/custom_pack/slash.nut"},
		{Job: 0, SkillIndex: 9, State: 249, ScriptPath: "sqr/custom_pack/followup.nut"},
	}
	if got := extractSkillStateCatalog(archive); !reflect.DeepEqual(got, want) {
		t.Fatalf("custom registrations=%+v want %+v", got, want)
	}
}

func TestNativeSwordWaveUsesMatchingDefinition(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/swordmanskill.lst":       {Data: []byte("20 `swordman/normalwave.skl`")},
		"skill/swordman/normalwave.skl": {Data: []byte("[type]\n`[active]`\n[consume MP]\n1 1\n[cool time]\n3000 3000")},
	}}
	got := extractSkillStateCatalog(a)
	if len(got) != 1 || got[0].SkillIndex != 20 || got[0].State != 24 || !reflect.DeepEqual(got[0].StateData, []int{20}) || !got[0].ItemCostFree || got[0].CooldownMS != 3000 {
		t.Fatalf("native wave definition=%+v", got)
	}
}
