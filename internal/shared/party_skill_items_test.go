package shared

import (
	"encoding/json"
	"testing"
)

func TestPartySkillItemCostComesOnlyFromEveryMatchingPVFDefinition(t *testing.T) {
	for _, path := range []string{"", "sqr/test.nut"} {
		for _, free := range []bool{false, true} {
			whitelist := []PartySkillState{{Job: 0, SkillIndex: 46, State: 28, ScriptPath: path, ItemCostFree: true}}
			pvf := []SkillState{{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "sqr/test.nut", ItemCostFree: free}}
			matches, _ := MatchPartySkillStates(0, whitelist, pvf)
			if len(matches) != 1 || matches[0].ItemCostFree != free {
				t.Fatalf("whitelist supplied material proof: %+v", matches)
			}
			pvf = append(pvf, SkillState{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "sqr/test.nut"})
			for range 2 {
				matches, _ = MatchPartySkillStates(0, whitelist, pvf)
				if len(matches) != 1 || matches[0].ItemCostFree {
					t.Fatalf("conflicting registration supplied material proof: %+v", matches)
				}
				pvf[0], pvf[1] = pvf[1], pvf[0]
			}
		}
	}
	whitelist := []PartySkillState{{Job: 0, SkillIndex: 46, State: 28, ItemCostFree: true}}
	pvf := []SkillState{
		{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "a", ItemCostFree: true},
		{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "b"},
	}
	matches, _ := MatchPartySkillStates(0, whitelist, pvf)
	if len(matches) != 1 || matches[0].ItemCostFree {
		t.Fatal("pathless match selected only its free registration")
	}
}

func TestOldSkillCatalogCannotSupplyMaterialProof(t *testing.T) {
	var entries []SkillState
	if err := json.Unmarshal([]byte(`[{"job":0,"skill_index":46,"state":28}]`), &entries); err != nil {
		t.Fatal(err)
	}
	SetSkillStates(entries)
	t.Cleanup(func() { SetSkillStates(nil) })
	got := SkillStatesForJob(0)
	if len(got) != 1 || got[0].ItemCostFree {
		t.Fatal("old cached catalog acquired free material cost")
	}
}
