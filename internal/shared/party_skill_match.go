package shared

import "strings"

// PartySkillMatchStats explains why whitelist entries did not become runtime
// candidates. The same counters are used by runtime logs and diagnostics.
type PartySkillMatchStats struct {
	PVFMatched          int
	SkippedMissingPVF   int
	SkippedPathMismatch int
}

// MatchPartySkillStates returns whitelist entries that exist in the PVF skill
// export for the requested job. A whitelist script path, when present, must
// match the normalized PVF path exactly.
func MatchPartySkillStates(job int, whitelist []PartySkillState, pvfStates []SkillState) ([]PartySkillState, PartySkillMatchStats) {
	pvfIndex := make(map[[3]int]map[string]struct{}, len(pvfStates))
	for _, entry := range pvfStates {
		if entry.Job != job || !validPartySkillKey(entry.SkillIndex, entry.State) {
			continue
		}
		key := [3]int{entry.Job, entry.SkillIndex, entry.State}
		paths := pvfIndex[key]
		if paths == nil {
			paths = make(map[string]struct{})
			pvfIndex[key] = paths
		}
		paths[normalizePartySkillScriptPath(entry.ScriptPath)] = struct{}{}
	}

	matches := make([]PartySkillState, 0, len(whitelist))
	stats := PartySkillMatchStats{}
	for _, entry := range whitelist {
		if entry.Job != job || !validPartySkillKey(entry.SkillIndex, entry.State) {
			continue
		}
		paths, ok := pvfIndex[[3]int{entry.Job, entry.SkillIndex, entry.State}]
		if !ok {
			stats.SkippedMissingPVF++
			continue
		}
		if scriptPath := normalizePartySkillScriptPath(entry.ScriptPath); scriptPath != "" {
			if _, ok := paths[scriptPath]; !ok {
				stats.SkippedPathMismatch++
				continue
			}
		}
		entry.StateData = append([]byte(nil), entry.StateData...)
		matches = append(matches, entry)
		stats.PVFMatched++
	}
	return matches, stats
}

func validPartySkillKey(skillIndex, state int) bool {
	return skillIndex > 0 && skillIndex <= 255 && state >= 0 && state <= 255
}

func normalizePartySkillScriptPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.Trim(value, "/")
	return strings.ToLower(value)
}
