package shared

import (
	"bytes"
	"testing"
)

func TestMatchPartySkillStatesUsesKeyAndNormalizedPath(t *testing.T) {
	whitelist := []PartySkillState{
		{Job: 6, SkillIndex: 3, State: 22, ScriptPath: `SQR\Character\Thief\skill.nut`, StateData: []byte{3, 0, 0}},
		{Job: 6, SkillIndex: 4, State: 23},
		{Job: 6, SkillIndex: 5, State: 24},
		{Job: 6, SkillIndex: 7, State: 26, ScriptPath: "expected/skill.nut"},
		{Job: 2, SkillIndex: 6, State: 25},
	}
	pvfStates := []SkillState{
		{Job: 6, SkillIndex: 3, State: 22, ScriptPath: "/sqr/character/thief/SKILL.NUT/"},
		{Job: 6, SkillIndex: 4, State: 23},
		{Job: 6, SkillIndex: 7, State: 26, ScriptPath: "other/skill.nut"},
		{Job: 2, SkillIndex: 6, State: 25},
	}

	matches, stats := MatchPartySkillStates(6, whitelist, pvfStates)
	if len(matches) != 2 || matches[0].SkillIndex != 3 || matches[1].SkillIndex != 4 {
		t.Fatalf("matches = %+v", matches)
	}
	if !bytes.Equal(matches[0].StateData, []byte{3, 0, 0}) {
		t.Fatalf("state data = %v", matches[0].StateData)
	}
	if stats.PVFMatched != 2 || stats.SkippedMissingPVF != 1 || stats.SkippedPathMismatch != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestMatchPartySkillStatesClonesStateData(t *testing.T) {
	whitelist := []PartySkillState{{Job: 1, SkillIndex: 2, State: 3, StateData: []byte{1, 2, 3}}}
	matches, _ := MatchPartySkillStates(1, whitelist, []SkillState{{Job: 1, SkillIndex: 2, State: 3}})
	matches[0].StateData[0] = 9
	if whitelist[0].StateData[0] != 1 {
		t.Fatal("matcher returned aliased state data")
	}
}
