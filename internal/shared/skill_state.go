package shared

import "sync/atomic"

type SkillState struct {
	Job          int    `json:"job"`
	SkillIndex   int    `json:"skill_index"`
	State        int    `json:"state"`
	Level        int    `json:"level,omitempty"`
	Name         string `json:"name,omitempty"`
	ScriptPath   string `json:"script_path"`
	StateData    []byte `json:"state_data,omitempty"`
	Verified     bool   `json:"verified,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
	Risk         int    `json:"risk,omitempty"`
}

var skillStateSnapshot atomic.Value
var partySkillStateSnapshot atomic.Value

func init() {
	skillStateSnapshot.Store([]SkillState(nil))
	partySkillStateSnapshot.Store([]PartySkillState(nil))
}

type PartySkillState struct {
	Job        int
	SkillIndex int
	State      int
	Level      int
	Name       string
	ScriptPath string
	StateData  []byte
	Risk       int
}

func SetSkillStates(entries []SkillState) {
	skillStateSnapshot.Store(cloneSkillStates(entries))
}

func SkillStatesForJob(job int) []SkillState {
	entries := skillStateSnapshot.Load().([]SkillState)
	out := make([]SkillState, 0)
	for _, entry := range entries {
		if entry.Job == job {
			entry.StateData = append([]byte(nil), entry.StateData...)
			out = append(out, entry)
		}
	}
	return out
}

func cloneSkillStates(entries []SkillState) []SkillState {
	out := append([]SkillState(nil), entries...)
	for i := range out {
		out[i].StateData = append([]byte(nil), out[i].StateData...)
	}
	return out
}

func SetPartySkillStates(entries []PartySkillState) {
	partySkillStateSnapshot.Store(clonePartySkillStates(entries))
}

func PartySkillStatesForJob(job int) []PartySkillState {
	entries := partySkillStateSnapshot.Load().([]PartySkillState)
	out := make([]PartySkillState, 0)
	for _, entry := range entries {
		if entry.Job == job {
			entry.StateData = append([]byte(nil), entry.StateData...)
			out = append(out, entry)
		}
	}
	return out
}

func clonePartySkillStates(entries []PartySkillState) []PartySkillState {
	out := append([]PartySkillState(nil), entries...)
	for i := range out {
		out[i].StateData = append([]byte(nil), out[i].StateData...)
	}
	return out
}
