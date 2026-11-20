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
	pvfIndex := make(map[[3]int]map[string]SkillState, len(pvfStates))
	for _, entry := range pvfStates {
		if entry.Job != job || !validPartySkillKey(entry.SkillIndex, entry.State) {
			continue
		}
		key := [3]int{entry.Job, entry.SkillIndex, entry.State}
		paths := pvfIndex[key]
		if paths == nil {
			paths = make(map[string]SkillState)
			pvfIndex[key] = paths
		}
		paths[normalizePartySkillScriptPath(entry.ScriptPath)] = entry
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
			cooldown, ok := paths[scriptPath]
			if !ok {
				stats.SkippedPathMismatch++
				continue
			}
			entry.CooldownMS = cooldown.CooldownMS
			entry.CooldownByLevelMS = append([]int(nil), cooldown.CooldownByLevelMS...)
		} else {
			// A legacy whitelist can omit the script path. Respect the longest
			// known baseline among its matching registrations.
			entry.CooldownMS = 0
			entry.CooldownByLevelMS = nil
			for _, cooldown := range paths {
				entry.CooldownMS = max(entry.CooldownMS, cooldown.CooldownMS)
				if len(cooldown.CooldownByLevelMS) > len(entry.CooldownByLevelMS) {
					entry.CooldownByLevelMS = append(entry.CooldownByLevelMS, make([]int, len(cooldown.CooldownByLevelMS)-len(entry.CooldownByLevelMS))...)
				}
			}
			for i := range entry.CooldownByLevelMS {
				value := entry.CooldownMS
				for _, cooldown := range paths {
					if table := cooldown.CooldownByLevelMS; len(table) > 0 {
						value = max(value, table[min(i, len(table)-1)])
					}
				}
				entry.CooldownByLevelMS[i] = value
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
