package shared

import (
	"bytes"
	"reflect"
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

func TestLegacySkillMatchKeepsLongestCooldownAtEachLevel(t *testing.T) {
	pvf := []SkillState{
		{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "a.nut", CooldownByLevelMS: []int{10000, 5000}},
		{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "b.nut", CooldownByLevelMS: []int{3000, 9000, 12000}},
		{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "c.nut", CooldownMS: 6000},
	}
	got, _ := MatchPartySkillStates(8, []PartySkillState{{Job: 8, SkillIndex: 26, State: 43}}, pvf)
	if len(got) != 1 || !reflect.DeepEqual(got[0].CooldownByLevelMS, []int{10000, 9000, 12000}) {
		t.Fatalf("legacy path selection lost a level's cooldown: %+v", got)
	}
	got[0].CooldownByLevelMS[0] = 1
	if pvf[0].CooldownByLevelMS[0] != 10000 {
		t.Fatal("matched cooldown table aliases PVF storage")
	}
}

func TestSkillCatalogCooldownSnapshotsRemainIndependent(t *testing.T) {
	previous := skillStateSnapshot.Load().([]SkillState)
	defer skillStateSnapshot.Store(previous)
	input := []SkillState{{Job: 8, SkillIndex: 26, State: 43, CooldownByLevelMS: []int{10000, 8000}}}
	SetSkillStates(input)
	input[0].CooldownByLevelMS[0] = 1
	read := SkillStatesForJob(8)
	read[0].CooldownByLevelMS[1] = 2
	if got := SkillStatesForJob(8); !reflect.DeepEqual(got[0].CooldownByLevelMS, []int{10000, 8000}) {
		t.Fatalf("a caller changed the published cooldown snapshot: %+v", got)
	}
}

func TestSkillCatalogBaseMPSnapshotsRemainIndependent(t *testing.T) {
	previous := skillStateSnapshot.Load().([]SkillState)
	defer skillStateSnapshot.Store(previous)
	input := []SkillState{{Job: 8, SkillIndex: 1, State: 20, BaseMPByLevel: []int{6, 8, 10}}}
	SetSkillStates(input)
	input[0].BaseMPByLevel[0] = 100
	read := SkillStatesForJob(8)
	read[0].BaseMPByLevel[1] = 200
	if got := SkillStatesForJob(8); !reflect.DeepEqual(got[0].BaseMPByLevel, []int{6, 8, 10}) {
		t.Fatalf("a caller changed the published base MP table: %+v", got)
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

func TestSkillStateSnapshotsPreserveUnknownVersusKnownEmptyVectors(t *testing.T) {
	previousSkills := skillStateSnapshot.Load().([]SkillState)
	previousParty := partySkillStateSnapshot.Load().(partySkillRuntime)
	t.Cleanup(func() {
		skillStateSnapshot.Store(previousSkills)
		partySkillStateSnapshot.Store(previousParty)
	})

	SetSkillStates([]SkillState{
		{Job: 8, SkillIndex: 1, State: 20, StateData: []int{}},
		{Job: 8, SkillIndex: 26, State: 43, StateData: nil},
	})
	states := SkillStatesForJob(8)
	if len(states) != 2 || states[0].StateData == nil || states[1].StateData != nil {
		t.Fatalf("PVF state snapshot lost vector knowledge: %+v", states)
	}

	SetPartySkillStatesWithMode([]PartySkillState{
		{Job: 8, SkillIndex: 1, State: 20, StateData: []byte{}},
		{Job: 8, SkillIndex: 26, State: 43, StateData: nil},
	}, true)
	partyStates := PartySkillStatesForJob(8)
	if len(partyStates) != 2 || partyStates[0].StateData == nil || partyStates[1].StateData != nil {
		t.Fatalf("party state snapshot lost vector knowledge: %+v", partyStates)
	}
}
