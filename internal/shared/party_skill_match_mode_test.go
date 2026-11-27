package shared

import "testing"

func TestMatchPartySkillStatesModeRequiresCurrentPVFRegistration(t *testing.T) {
	whitelist := []PartySkillState{
		{Job: 4, SkillIndex: 240, State: 240, Level: 10, StateData: []byte{1}, ScriptPath: "sqr/priest/skill.nut"},
		{Job: 4, SkillIndex: 241, State: 241, Level: 10, StateData: []byte{0, 1}, ScriptPath: "sqr/priest/unknown.nut"},
	}
	pvfStates := []SkillState{{Job: 4, SkillIndex: 240, State: 240, ScriptPath: "sqr/priest/skill.nut", CooldownMS: 5000}}

	strict, strictStats := MatchPartySkillStates(4, whitelist, pvfStates)
	if len(strict) != 1 || strict[0].CooldownMS != 5000 {
		t.Fatalf("strict = %+v", strict)
	}
	if strictStats.SkippedMissingPVF != 1 || strictStats.KeptWithoutPVF != 0 {
		t.Fatalf("strict stats = %+v", strictStats)
	}

	auto, autoStats := MatchPartySkillStatesMode(true, 4, whitelist, pvfStates)
	if len(auto) != 1 || auto[0].SkillIndex != 240 {
		t.Fatalf("auto = %+v", auto)
	}
	if autoStats.PVFMatched != 1 || autoStats.KeptWithoutPVF != 0 || autoStats.SkippedMissingPVF != 1 {
		t.Fatalf("auto stats = %+v", autoStats)
	}
	if auto[0].CooldownMS != 5000 {
		t.Fatalf("registered cooldown = %d", auto[0].CooldownMS)
	}
}

func TestMatchPartySkillStatesModeRejectsPathMismatchAndEmptyVectors(t *testing.T) {
	whitelist := []PartySkillState{
		{Job: 2, SkillIndex: 5, State: 96, Level: 10, StateData: []byte{0}, ScriptPath: "sqr/wrong/path.nut"},
		{Job: 2, SkillIndex: 6, State: 97, Level: 10},
	}
	pvfStates := []SkillState{{Job: 2, SkillIndex: 5, State: 96, ScriptPath: "sqr/gunner/path.nut"}}
	auto, stats := MatchPartySkillStatesMode(true, 2, whitelist, pvfStates)
	if len(auto) != 0 {
		t.Fatalf("auto = %+v", auto)
	}
	if stats.SkippedPathMismatch != 1 || stats.SkippedMissingPVF != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestAutoMatchRequiresKnownVectorAndPreservesKnownEmpty(t *testing.T) {
	whitelist := []PartySkillState{
		{Job: 8, SkillIndex: 1, State: 20, ScriptPath: "sqr/character/atmage/windstrike/wind_strike.nut", StateData: []byte{}},
		{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "sqr/character/atmage/watercannon.nut"},
	}
	pvfStates := []SkillState{
		{Job: 8, SkillIndex: 1, State: 20, ScriptPath: "sqr/character/atmage/windstrike/wind_strike.nut"},
		{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "sqr/character/atmage/watercannon.nut"},
	}

	auto, stats := MatchPartySkillStatesMode(true, 8, whitelist, pvfStates)
	if len(auto) != 1 || auto[0].SkillIndex != 1 || auto[0].StateData == nil || len(auto[0].StateData) != 0 {
		t.Fatalf("auto candidates did not preserve the known empty vector: %+v", auto)
	}
	if stats.PVFMatched != 1 || stats.SkippedUnknownStateData != 1 {
		t.Fatalf("auto stats = %+v", stats)
	}

	manual, _ := MatchPartySkillStatesMode(false, 8, whitelist, pvfStates)
	if len(manual) != 2 || manual[1].StateData != nil {
		t.Fatalf("manual compatibility changed: %+v", manual)
	}
}

func TestPartySkillAutoSelectionFollowsPublishedSnapshot(t *testing.T) {
	previousAuto := PartySkillAutoSelection()
	previousEntries := PartySkillStatesForJob(3)
	t.Cleanup(func() {
		SetPartySkillStatesWithMode(previousEntries, previousAuto)
	})
	SetPartySkillStatesWithMode([]PartySkillState{{Job: 3, SkillIndex: 1, State: 20}}, true)
	if !PartySkillAutoSelection() {
		t.Fatal("auto flag was not published")
	}
	SetPartySkillStates([]PartySkillState{{Job: 3, SkillIndex: 1, State: 20}})
	if PartySkillAutoSelection() {
		t.Fatal("manual publication kept the auto flag")
	}
}
