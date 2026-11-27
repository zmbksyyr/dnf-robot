package shared

import (
	"fmt"
	"slices"
	"sync/atomic"
)

type SkillState struct {
	Job               int    `json:"job"`
	SkillIndex        int    `json:"skill_index"`
	State             int    `json:"state"`
	ScriptPath        string `json:"script_path"`
	CooldownMS        int    `json:"cooldown_ms,omitempty"`
	CooldownByLevelMS []int  `json:"cooldown_by_level_ms,omitempty"`
	// Base MP excludes equipment, passive and runtime skill modifiers. A nil
	// table is unknown; one entry is constant at every learned level (zero is
	// an explicitly free base cost).
	BaseMPByLevel []int `json:"base_mp_by_level,omitempty"`
	// These are definition-level rules, not the complete equipment/passive
	// modifiers or an effective cost. Old catalogs leave Known false.
	MPRules SkillMPRules `json:"mp_rules"`
	// True only when the decoded definition has no native material entries in
	// any mode. False also covers missing definitions and older catalogs.
	ItemCostFree bool `json:"item_cost_free"`
	// Raw material definition only; Known does not establish effective costs.
	Materials SkillMaterialDefinition `json:"materials"`
	// StateData holds captured native values or a constant initial vector read
	// from the registered script entry. Nil means unknown; a non-nil empty slice
	// is a known empty vector and must survive catalog serialization.
	StateData []int `json:"native_state_data"`
}

type SkillMPRules struct {
	Known                    bool `json:"known"`
	WeaponEffect             byte `json:"weapon_effect"` // 0: none, 1: physical, 2: magical
	CommandReductionPermille int  `json:"command_reduction_permille,omitempty"`
}

var skillStateSnapshot atomic.Value
var partySkillStateSnapshot atomic.Value
var partySkillStateGeneration atomic.Uint64

type partySkillRuntime struct {
	entries []PartySkillState
	enabled bool
	auto    bool
}

func init() {
	skillStateSnapshot.Store([]SkillState(nil))
	partySkillStateSnapshot.Store(partySkillRuntime{})
}

type PartySkillState struct {
	Job        int
	SkillIndex int
	State      int
	Level      int
	Name       string
	ScriptPath string
	// StateData is the encoded initial vector. Nil means unknown; non-nil empty
	// means an observed or explicit zero-length vector.
	StateData         []byte
	Risk              int
	CooldownMS        int
	CooldownByLevelMS []int
	// Resolved from the matching PVF registration, never from the whitelist.
	// These remain base definitions; they do not establish an effective cost.
	BaseMPByLevel []int
	MPRules       SkillMPRules
	ItemCostFree  bool
	Materials     SkillMaterialDefinition
}

// SET_STATE prefixes the encoded vector's byte length with an unsigned word.
// Transport MTU and reliable-frame limits are checked separately at send time.
const MaxPartySkillStateDataSize = 65535

// An empty swordman slash state reproducibly terminates the client. The
// observed non-empty state avoids that failure; this check does not claim to
// validate every skill's parameter semantics.
func ValidatePartySkillStateData(job, state int, data []byte) error {
	if len(data) > MaxPartySkillStateDataSize {
		return fmt.Errorf("state_data encoded size %d exceeds %d bytes", len(data), MaxPartySkillStateDataSize)
	}
	if job == 0 && state == 23 && len(data) == 0 {
		return fmt.Errorf("swordman state 23 requires non-empty state_data")
	}
	return nil
}

func SetSkillStates(entries []SkillState) {
	skillStateSnapshot.Store(cloneSkillStates(entries))
	// Loaded profiles cache these resource definitions as well as the party
	// catalog. Replacing or clearing either input invalidates the profile.
	partySkillStateGeneration.Add(1)
}

func SkillStatesForJob(job int) []SkillState {
	entries := skillStateSnapshot.Load().([]SkillState)
	out := make([]SkillState, 0)
	for _, entry := range entries {
		if entry.Job == job {
			entry.CooldownByLevelMS = append([]int(nil), entry.CooldownByLevelMS...)
			entry.BaseMPByLevel = append([]int(nil), entry.BaseMPByLevel...)
			entry.StateData = slices.Clone(entry.StateData)
			entry.Materials = entry.Materials.Clone()
			out = append(out, entry)
		}
	}
	return out
}

func cloneSkillStates(entries []SkillState) []SkillState {
	out := append([]SkillState(nil), entries...)
	for i := range out {
		out[i].CooldownByLevelMS = append([]int(nil), out[i].CooldownByLevelMS...)
		out[i].BaseMPByLevel = append([]int(nil), out[i].BaseMPByLevel...)
		out[i].StateData = slices.Clone(out[i].StateData)
		out[i].Materials = out[i].Materials.Clone()
	}
	return out
}

func SetPartySkillStates(entries []PartySkillState) {
	SetPartySkillStatesWithMode(entries, false)
}

// SetPartySkillStatesWithMode publishes the runtime skill snapshot together
// with the selection mode that produced it, so consumers never match an entry
// set against the wrong eligibility rule.
func SetPartySkillStatesWithMode(entries []PartySkillState, auto bool) {
	SetPartySkillRuntime(entries, entries != nil, auto)
}

// SetPartySkillRuntime keeps the master switch separate from an empty active
// skill list: enabled characters can still use their native normal attack.
// Publish flags and entries together so a reload cannot mix configurations.
func SetPartySkillRuntime(entries []PartySkillState, enabled, auto bool) {
	if !enabled {
		entries = nil
	}
	partySkillStateSnapshot.Store(partySkillRuntime{entries: clonePartySkillStates(entries), enabled: enabled, auto: enabled && auto})
	partySkillStateGeneration.Add(1)
}

func PartySkillEnabled() bool {
	return partySkillStateSnapshot.Load().(partySkillRuntime).enabled
}

// PartySkillAutoSelection reports whether the published snapshot was built by
// automatic selection. Both modes still require matching PVF registrations.
func PartySkillAutoSelection() bool {
	return partySkillStateSnapshot.Load().(partySkillRuntime).auto
}

func PartySkillStateGeneration() uint64 {
	return partySkillStateGeneration.Load()
}

func PartySkillStatesForJob(job int) []PartySkillState {
	entries := partySkillStateSnapshot.Load().(partySkillRuntime).entries
	out := make([]PartySkillState, 0)
	for _, entry := range entries {
		if entry.Job == job {
			entry.StateData = slices.Clone(entry.StateData)
			entry.CooldownByLevelMS = append([]int(nil), entry.CooldownByLevelMS...)
			entry.BaseMPByLevel = append([]int(nil), entry.BaseMPByLevel...)
			entry.Materials = entry.Materials.Clone()
			out = append(out, entry)
		}
	}
	return out
}

func clonePartySkillStates(entries []PartySkillState) []PartySkillState {
	out := append([]PartySkillState(nil), entries...)
	for i := range out {
		out[i].StateData = slices.Clone(out[i].StateData)
		out[i].CooldownByLevelMS = append([]int(nil), out[i].CooldownByLevelMS...)
		out[i].BaseMPByLevel = append([]int(nil), out[i].BaseMPByLevel...)
		out[i].Materials = out[i].Materials.Clone()
	}
	return out
}
