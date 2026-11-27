package shared

import (
	"reflect"
	"testing"
)

func TestPartySkillMatchTakesMPFromMatchingPVFPath(t *testing.T) {
	rules := SkillMPRules{Known: true, WeaponEffect: 2, CommandReductionPermille: 20}
	whitelist := []PartySkillState{{Job: 0, SkillIndex: 46, State: 28, StateData: []byte{2, 46}, ScriptPath: "right.nut", BaseMPByLevel: []int{0}, MPRules: SkillMPRules{Known: true}}}
	pvf := []SkillState{
		{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "wrong.nut", BaseMPByLevel: []int{999}},
		{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "right.nut", BaseMPByLevel: []int{1, 2}, MPRules: rules},
	}
	for _, auto := range []bool{false, true} {
		got, _ := MatchPartySkillStatesMode(auto, 0, whitelist, pvf)
		if len(got) != 1 || !reflect.DeepEqual(got[0].BaseMPByLevel, []int{1, 2}) || got[0].MPRules != rules {
			t.Fatalf("auto=%t resource definition = %+v", auto, got)
		}
		got[0].BaseMPByLevel[0] = 500
		if pvf[1].BaseMPByLevel[0] != 1 || whitelist[0].BaseMPByLevel[0] != 0 {
			t.Fatal("matched resources alias the source catalog")
		}
	}
}

func TestAmbiguousPartySkillMPRemainsUnknown(t *testing.T) {
	rules := SkillMPRules{Known: true, WeaponEffect: 1}
	for _, duplicatePath := range []bool{false, true} {
		for _, conflict := range []string{"none", "base", "rules", "missing"} {
			t.Run(conflict+map[bool]string{false: "/legacy", true: "/duplicate"}[duplicatePath], func(t *testing.T) {
				a := SkillState{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "a.nut", BaseMPByLevel: []int{0}, MPRules: rules}
				b := a
				entry := PartySkillState{Job: 0, SkillIndex: 46, State: 28}
				if duplicatePath {
					entry.ScriptPath = "a.nut"
				} else {
					b.ScriptPath = "b.nut"
				}
				switch conflict {
				case "base":
					b.BaseMPByLevel = []int{1}
				case "rules":
					b.MPRules.WeaponEffect = 2
				case "missing":
					b.BaseMPByLevel = nil
					b.MPRules = SkillMPRules{}
				}
				for _, pvf := range [][]SkillState{{a, b}, {b, a}} {
					got, _ := MatchPartySkillStates(0, []PartySkillState{entry}, pvf)
					if len(got) != 1 {
						t.Fatalf("matches = %+v", got)
					}
					wantBase := []int{0}
					if conflict == "base" || conflict == "missing" {
						wantBase = nil
					}
					wantRules := rules
					if conflict == "rules" || conflict == "missing" {
						wantRules = SkillMPRules{}
					}
					if !reflect.DeepEqual(got[0].BaseMPByLevel, wantBase) || got[0].MPRules != wantRules {
						t.Fatalf("conflicting resources became known: %+v", got[0])
					}
				}
			})
		}
	}
}

func TestPartySkillMPSnapshotDoesNotAliasCallers(t *testing.T) {
	previous := partySkillStateSnapshot.Load().(partySkillRuntime)
	defer partySkillStateSnapshot.Store(previous)
	input := []PartySkillState{{Job: 0, SkillIndex: 46, BaseMPByLevel: []int{1, 2}}}
	SetPartySkillRuntime(input, true, false)
	input[0].BaseMPByLevel[0] = 50
	read := PartySkillStatesForJob(0)
	read[0].BaseMPByLevel[1] = 60
	if got := PartySkillStatesForJob(0); !reflect.DeepEqual(got[0].BaseMPByLevel, []int{1, 2}) {
		t.Fatalf("published MP changed: %+v", got)
	}
}
