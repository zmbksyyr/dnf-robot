package pvf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"robot/internal/capability/catalog"
)

func TestScriptEntryVectorReachesExistingAutoCatalog(t *testing.T) {
	// Same entry sequence as sqr/character/atmage/chainlightning/chainlightning.nut:
	// the vector belongs to checkExecutableSkill, not a later onSetState phase.
	a := &pvfArchive{files: map[string]*pvfFile{
		"sqr/character/example_load_state.nut": {Data: []byte(`
IRDSQRCharacter.pushScriptFiles("character/header.nut");
IRDSQRCharacter.pushState(8, "character/skill.nut", "ChainLightning", STATE_CHAINLIGHTNING, 2);
`)},
		"sqr/character/header.nut": {Data: []byte("STATE_CHAINLIGHTNING <- 25;")},
		"sqr/character/skill.nut": {Data: []byte(`
SUB_STATE_ATCHAINLIGHTNING_0 <- 0
function checkExecutableSkill_ChainLightning(obj) {
    if (!obj) return false;
    local ready = obj.sq_IsUseSkill(2);
    if (ready) {
        obj.sq_IntVectClear();
        obj.sq_IntVectPush(SUB_STATE_ATCHAINLIGHTNING_0);
        obj.sq_AddSetStatePacket(STATE_CHAINLIGHTNING, STATE_PRIORITY_IGNORE_FORCE, true);
        return true;
    }
    return false;
}

function onEndCurrentAni_ChainLightning(obj) {
    obj.sq_IntVectClear(); obj.sq_IntVectPush(1);
    obj.sq_AddSetStatePacket(STATE_CHAINLIGHTNING, STATE_PRIORITY_USER, true);
}`)},
	}}
	states := extractSkillStateCatalog(a)
	if len(states) != 1 || !reflect.DeepEqual(states[0].StateData, []int{0}) {
		t.Fatalf("original entry vector not exported: %+v", states)
	}
	dir := t.TempDir()
	path, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
	// Existing auto catalogs were generated without a state_data field.
	if err := os.WriteFile(path, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[
{"enabled":true,"job":8,"skill_index":2,"state":25,"level":70,"script_path":"sqr/character/skill.nut"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(pvfPath, states); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SyncPartySkillCatalog(path, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ReadPartySkillCatalog(path)
	if err != nil || len(report.Issues) != 0 || len(report.Entries) != 1 || !reflect.DeepEqual(report.Entries[0].StateData, []byte{0}) {
		t.Fatalf("original entry vector not adopted: %+v err=%v", report, err)
	}
}

func TestDynamicScriptEntryDoesNotBecomeAnEmptyAutoCast(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"sqr/test_load_state.nut": {Data: []byte(`IRDSQRCharacter.pushState(8,"skill.nut","Dynamic",25,2);`)},
		"sqr/skill.nut": {Data: []byte(`
function checkExecutableSkill_Dynamic(obj) {
    if (obj.sq_IsUseSkill(2)) {
        obj.sq_IntVectClear();
        obj.sq_IntVectPush(obj.getXPos());
        obj.sq_AddSetStatePacket(25,0,true);
    }
}`)},
	}}
	states := extractSkillStateCatalog(a)
	if len(states) != 1 || states[0].StateData != nil {
		t.Fatalf("dynamic entry was assigned constant parameters: %+v", states)
	}
	dir := t.TempDir()
	path, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
	if err := os.WriteFile(path, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(pvfPath, states); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SyncPartySkillCatalog(path, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ReadPartySkillCatalog(path)
	if err != nil || len(report.Issues) != 0 || !report.Auto() || len(report.Entries) != 0 {
		t.Fatalf("dynamic parameters became a castable empty vector: %+v err=%v", report, err)
	}
	if changed, err := catalog.SyncPartySkillCatalog(path, pvfPath); err != nil || changed {
		t.Fatalf("unchanged source did not keep the catalog stable: changed=%t err=%v", changed, err)
	}
}

func TestSkillSymbolsRejectConflictsAndNonDeclarations(t *testing.T) {
	got := skillStateSymbols(map[string]string{
		"a.nut": `
// STATE_COMMENT <- 91;
/* STATE_BLOCK <- 92; */
local example = "STATE_STRING <- 93;";
STATE_GOOD <- 25;
STATE_CONFLICT <- 26;
STATE_EXPRESSION <- 1 + 2;
function helper() { local STATE_LOCAL = 94; }
`,
		"b.nut": "STATE_GOOD <- 25;\nSTATE_CONFLICT <- 27;",
	})
	if got["STATE_GOOD"] != 25 {
		t.Fatal("unambiguous root constant missing")
	}
	for _, name := range []string{"STATE_COMMENT", "STATE_BLOCK", "STATE_STRING", "STATE_CONFLICT", "STATE_EXPRESSION", "STATE_LOCAL"} {
		if value, ok := got[name]; ok {
			t.Fatalf("ambiguous or nonconstant %s resolved to %d", name, value)
		}
	}
}

func TestScriptEntryAliasesRequireIdenticalVectors(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		want         []int
	}{
		{"same", `obj.sq_IntVectPush(0);`, []int{0}},
		{"conflict", `obj.sq_IntVectPush(5);`, nil},
		{"unknown", `obj.sq_IntVectPush(obj.getXPos());`, nil},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/forward", true: "/reverse"}[reverse], func(t *testing.T) {
				first := `IRDSQRCharacter.pushState(8,"skill.nut","First",25,2);`
				second := `IRDSQRCharacter.pushState(8,"skill.nut","Second",25,2);`
				registration := first + second + first
				if reverse {
					registration = second + first + second
				}
				a := &pvfArchive{files: map[string]*pvfFile{
					"sqr/test_load_state.nut": {Data: []byte(registration)},
					"sqr/skill.nut": {Data: []byte(`
function checkExecutableSkill_First(obj) {
    obj.sq_IntVectClear(); obj.sq_IntVectPush(0); obj.sq_AddSetStatePacket(25,0,true);
}
function checkExecutableSkill_Second(obj) {
    obj.sq_IntVectClear(); ` + tc.second + ` obj.sq_AddSetStatePacket(25,0,true);
}`)},
				}}
				got := extractSkillStateCatalog(a)
				if len(got) != 1 || !reflect.DeepEqual(got[0].StateData, tc.want) {
					t.Fatalf("alias vector=%+v want=%v", got, tc.want)
				}
			})
		}
	}
}

func TestScriptEntryAliasesDoNotTreatUnknownAsKnownEmpty(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		first := `IRDSQRCharacter.pushState(8,"skill.nut","Known",25,2);`
		second := `IRDSQRCharacter.pushState(8,"skill.nut","Unknown",25,2);`
		registration := first + second + first
		if reverse {
			registration = second + first + second
		}
		a := &pvfArchive{files: map[string]*pvfFile{
			"sqr/test_load_state.nut": {Data: []byte(registration)},
			"sqr/skill.nut": {Data: []byte(`
function checkExecutableSkill_Known(obj) {
    obj.sq_IntVectClear(); obj.sq_AddSetStatePacket(25,0,true);
}
function checkExecutableSkill_Unknown(obj) {
    obj.sq_AddSetStatePacket(25,0,true);
}`)},
		}}
		got := extractSkillStateCatalog(a)
		if len(got) != 1 || got[0].StateData != nil {
			t.Fatalf("unknown alias became known empty: %+v", got)
		}
	}
}

func TestScriptEntryVectorRejectsUnknownOrAmbiguousSequences(t *testing.T) {
	const start = `obj.sq_IntVectClear();`
	const push = `obj.sq_IntVectPush(SUB_START);`
	const send = `obj.sq_AddSetStatePacket(STATE_SKILL, STATE_PRIORITY_USER, true);`
	for _, tc := range []struct {
		name, body string
		want       []int
	}{
		{"constant", start + push + send, []int{0}},
		{"explicitly cleared empty", start + send, []int{}},
		{"unresolved implicit vector", send, nil},
		{"int32 vector", start + `obj.sq_IntVectPush(-2147483648); obj.sq_IntVectPush(-1); obj.sq_IntVectPush(2147483647); obj.sq_IntVectPush(0x1000000);` + send, []int{-2147483648, -1, 2147483647, 16777216}},
		{"comments", `/* obj.sq_AddSetStatePacket(99,0,true); */` + start + `// obj.sq_IntVectPush(8);
` + push + send, []int{0}},
		{"string", `local example = "obj.sq_AddSetStatePacket(99,0,true);";` + start + push + send, []int{0}},
		{"verbatim string", `local example = @"text "" obj.sq_AddSetStatePacket(99,0,true);";` + start + push + send, []int{0}},
		{"conditional clear", `if (ready) ` + start + push + send, nil},
		{"conditional push", start + `if (ready) {` + push + `}` + send, nil},
		{"alternative sends", `if (ready) {` + start + push + send + `} else {` + start + push + send + `}`, nil},
		{"helper mutates vector", start + `helper(obj);` + push + send, nil},
		{"different receiver", start + `other.sq_IntVectPush(0);` + send, nil},
		{"receiver rebound", `obj = other;` + start + push + send, nil},
		{"dynamic", start + `obj.sq_IntVectPush(obj.getXPos());` + send, nil},
		{"expression", start + `obj.sq_IntVectPush(1 + 2);` + send, nil},
		{"out of int32", start + `obj.sq_IntVectPush(2147483648);` + send, nil},
		{"missing clear", push + send, nil},
		{"different state", start + push + `obj.sq_AddSetStatePacket(26,0,true);`, nil},
		{"shadowed symbol", `local SUB_START;` + start + push + send, nil},
		{"shadowed list symbol", `local first, SUB_START;` + start + push + send, nil},
		{"nested function", `function nested() {` + start + push + send + `}`, nil},
		{"unterminated comment", start + push + send + `/*`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := "function checkExecutableSkill_Skill(obj) {" + tc.body + "}"
			got := scriptEntryStateData(text, "Skill", 25, map[string]int{"SUB_START": 0, "STATE_SKILL": 25})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("vector=%v want=%v", got, tc.want)
			}
		})
	}
	if got := scriptEntryStateData("function checkExecutableSkill_Other(obj) {"+start+push+send+"}", "Skill", 25, nil); got != nil {
		t.Fatalf("unregistered function supplied parameters: %v", got)
	}
}
