package shared

import "sync/atomic"

type SkillState struct {
	Job        int    `json:"job"`
	SkillIndex int    `json:"skill_index"`
	State      int    `json:"state"`
	ScriptPath string `json:"script_path"`
}

var skillStateSnapshot atomic.Value

func init() {
	skillStateSnapshot.Store([]SkillState(nil))
}

func SetSkillStates(entries []SkillState) {
	skillStateSnapshot.Store(append([]SkillState(nil), entries...))
}

func SkillStatesForJob(job int) []SkillState {
	entries := skillStateSnapshot.Load().([]SkillState)
	out := make([]SkillState, 0)
	for _, entry := range entries {
		if entry.Job == job {
			out = append(out, entry)
		}
	}
	return out
}
