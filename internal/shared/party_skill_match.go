package shared

import (
	"slices"
	"strings"
)

// PartySkillMatchStats explains why whitelist entries did not become runtime
// candidates. The same counters are used by runtime logs and diagnostics.
type PartySkillMatchStats struct {
	PVFMatched              int
	KeptWithoutPVF          int
	SkippedMissingPVF       int
	SkippedPathMismatch     int
	SkippedUnknownStateData int
}

// MatchPartySkillStates keeps the historical strict intersection: an entry only
// becomes a candidate when the PVF export registers the same job, skill, state
// and (when declared) script path.
func MatchPartySkillStates(job int, whitelist []PartySkillState, pvfStates []SkillState) ([]PartySkillState, PartySkillMatchStats) {
	return MatchPartySkillStatesMode(false, job, whitelist, pvfStates)
}

// MatchPartySkillStatesMode selects runtime candidates for the requested mode.
//
// Both modes require current PVF registrations. Automatic selection also needs
// an explicitly known vector; manual selection retains omitted-parameter
// compatibility. Parameters alone do not establish PVF compatibility.
func MatchPartySkillStatesMode(auto bool, job int, whitelist []PartySkillState, pvfStates []SkillState) ([]PartySkillState, PartySkillMatchStats) {
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
		path := normalizePartySkillScriptPath(entry.ScriptPath)
		if previous, exists := paths[path]; exists {
			// Multiple registrations must agree before their resource definition
			// can be used. Catalog order must not select a cheaper interpretation.
			entry.BaseMPByLevel, entry.MPRules = commonSkillMPDefinition(previous, entry)
			entry.ItemCostFree = previous.ItemCostFree && entry.ItemCostFree
			entry.Materials = commonSkillMaterialDefinition(previous.Materials, entry.Materials)
		}
		paths[path] = entry
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
			entry.BaseMPByLevel = append([]int(nil), cooldown.BaseMPByLevel...)
			entry.MPRules = cooldown.MPRules
			entry.ItemCostFree = cooldown.ItemCostFree
			entry.Materials = cooldown.Materials.Clone()
		} else {
			// A legacy whitelist can omit the script path. Respect the longest
			// known baseline among its matching registrations.
			entry.CooldownMS = 0
			entry.CooldownByLevelMS = nil
			var resource SkillState
			first := true
			for _, cooldown := range paths {
				if first {
					resource = cooldown
					first = false
				} else {
					resource.BaseMPByLevel, resource.MPRules = commonSkillMPDefinition(resource, cooldown)
					resource.ItemCostFree = resource.ItemCostFree && cooldown.ItemCostFree
					resource.Materials = commonSkillMaterialDefinition(resource.Materials, cooldown.Materials)
				}
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
			entry.BaseMPByLevel = append([]int(nil), resource.BaseMPByLevel...)
			entry.MPRules = resource.MPRules
			entry.ItemCostFree = resource.ItemCostFree
			entry.Materials = resource.Materials.Clone()
		}
		if auto && entry.StateData == nil {
			stats.SkippedUnknownStateData++
			continue
		}
		entry.StateData = slices.Clone(entry.StateData)
		matches = append(matches, entry)
		stats.PVFMatched++
	}
	return matches, stats
}

func commonSkillMPDefinition(a, b SkillState) ([]int, SkillMPRules) {
	base := a.BaseMPByLevel
	if !slices.Equal(base, b.BaseMPByLevel) {
		base = nil
	}
	rules := a.MPRules
	if rules != b.MPRules {
		rules = SkillMPRules{}
	}
	return base, rules
}

func validPartySkillKey(skillIndex, state int) bool {
	return skillIndex > 0 && skillIndex <= 255 && state >= 0 && state <= 255
}

func normalizePartySkillScriptPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.Trim(value, "/")
	return strings.ToLower(value)
}
