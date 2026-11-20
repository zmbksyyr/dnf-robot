package shared

import "testing"

func TestMatchPartySkillStatesModeKeepsCapturedVectorWithoutPVF(t *testing.T) {
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
	if len(auto) != 2 {
		t.Fatalf("auto = %+v", auto)
	}
	if autoStats.PVFMatched != 1 || autoStats.KeptWithoutPVF != 1 || autoStats.SkippedMissingPVF != 0 {
		t.Fatalf("auto stats = %+v", autoStats)
	}
	// The PVF-registered entry keeps its cooldown; the captured-only entry does
	// not invent one.
	for _, entry := range auto {
		switch entry.SkillIndex {
		case 240:
			if entry.CooldownMS != 5000 {
				t.Fatalf("registered cooldown = %d", entry.CooldownMS)
			}
		case 241:
			if entry.CooldownMS != 0 || len(entry.CooldownByLevelMS) != 0 {
				t.Fatalf("captured-only cooldown = %d/%v", entry.CooldownMS, entry.CooldownByLevelMS)
			}
			if len(entry.StateData) != 2 || entry.StateData[0] != 0 || entry.StateData[1] != 1 {
				t.Fatalf("captured vector changed: %x", entry.StateData)
			}
		}
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
