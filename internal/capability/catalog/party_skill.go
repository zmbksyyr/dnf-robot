package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"robot/internal/foundation/atomicfile"
	foundationconfig "robot/internal/foundation/config"
	"robot/internal/shared"
)

const maxSafePartySkillLevel = 70

type partySkillCatalogEntry struct {
	Disabled   bool            `json:"disabled,omitempty"`
	Job        int             `json:"job"`
	SkillIndex int             `json:"skill_index"`
	State      int             `json:"state"`
	Level      int             `json:"level"`
	Name       string          `json:"name,omitempty"`
	ScriptPath string          `json:"script_path,omitempty"`
	StateData  json.RawMessage `json:"state_data,omitempty"`
	Risk       int             `json:"risk,omitempty"`
}

type partySkillCatalogDocument struct {
	Enabled       *bool              `json:"enabled"`
	MaxSkillLevel *int               `json:"max_skill_level"`
	Skills        *[]json.RawMessage `json:"skills"`
}

type partySkillCatalogEntryDocument struct {
	Disabled   bool             `json:"disabled,omitempty"`
	Job        *int             `json:"job"`
	SkillIndex *int             `json:"skill_index"`
	State      *int             `json:"state"`
	Level      *int             `json:"level"`
	Name       string           `json:"name,omitempty"`
	ScriptPath string           `json:"script_path,omitempty"`
	StateData  *json.RawMessage `json:"state_data,omitempty"`
	Risk       int              `json:"risk,omitempty"`
}

type PartySkillCatalogIssue struct {
	Index      int
	Job        int
	SkillIndex int
	State      int
	Reason     string
}

type PartySkillCatalogReport struct {
	Enabled                 bool
	Entries                 []shared.PartySkillState
	Issues                  []PartySkillCatalogIssue
	SourceCount             int
	DisabledCount           int
	OverLevelCount          int
	ConfiguredMaxSkillLevel int
	EffectiveMaxSkillLevel  int
}

type PartySkillCatalogValidationError struct {
	Issues []PartySkillCatalogIssue
}

func (e *PartySkillCatalogValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return ""
	}
	const detailLimit = 3
	details := make([]string, 0, min(len(e.Issues), detailLimit))
	for _, issue := range e.Issues[:min(len(e.Issues), detailLimit)] {
		details = append(details, fmt.Sprintf("entry=%d job=%d skill=%d state=%d: %s", issue.Index, issue.Job, issue.SkillIndex, issue.State, issue.Reason))
	}
	suffix := ""
	if remaining := len(e.Issues) - len(details); remaining > 0 {
		suffix = fmt.Sprintf("; and %d more", remaining)
	}
	return fmt.Sprintf("party skill catalog has %d invalid entries: %s%s", len(e.Issues), strings.Join(details, "; "), suffix)
}

func LoadPartySkills(configDir string) error {
	if strings.TrimSpace(configDir) == "" {
		return fmt.Errorf("empty party skill catalog directory")
	}
	report, err := ReadPartySkillCatalog(filepath.Join(configDir, "party_skill_catalog.json"))
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return &PartySkillCatalogValidationError{Issues: report.Issues}
	}
	if !report.Enabled {
		shared.SetPartySkillStates(nil)
		return nil
	}
	shared.SetPartySkillStates(report.Entries)
	return nil
}

// ReadPartySkillCatalog parses and validates the whitelist without changing
// the process-wide runtime snapshot. Entries with semantic errors are reported
// in Issues; a runtime publisher must reject the whole snapshot when Issues is
// non-empty.
func ReadPartySkillCatalog(path string) (PartySkillCatalogReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PartySkillCatalogReport{}, err
	}
	return parsePartySkillCatalog(data)
}

func parsePartySkillCatalog(data []byte) (PartySkillCatalogReport, error) {
	raw, err := decodePartySkillCatalogDocument(data)
	if err != nil {
		return PartySkillCatalogReport{}, err
	}

	report := PartySkillCatalogReport{
		Enabled:                 *raw.Enabled,
		SourceCount:             len(*raw.Skills),
		ConfiguredMaxSkillLevel: *raw.MaxSkillLevel,
		EffectiveMaxSkillLevel:  *raw.MaxSkillLevel,
		Entries:                 make([]shared.PartySkillState, 0, len(*raw.Skills)),
	}
	for index, rawEntry := range *raw.Skills {
		entry, err := decodePartySkillCatalogEntry(index, rawEntry)
		if err != nil {
			return PartySkillCatalogReport{}, err
		}
		if reason := invalidPartySkillEntryReason(entry); reason != "" {
			report.addPartySkillIssue(index, entry, reason)
			continue
		}
		values, err := decodePartySkillStateData(entry.StateData)
		if err != nil {
			report.addPartySkillIssue(index, entry, err.Error())
			continue
		}
		stateData, err := partySkillStateData(values)
		if err != nil {
			report.addPartySkillIssue(index, entry, err.Error())
			continue
		}
		if entry.Disabled {
			report.DisabledCount++
			continue
		}
		if entry.Level > report.EffectiveMaxSkillLevel {
			report.OverLevelCount++
			continue
		}
		report.Entries = append(report.Entries, shared.PartySkillState{
			Job: entry.Job, SkillIndex: entry.SkillIndex, State: entry.State,
			Level: entry.Level, Name: entry.Name, ScriptPath: entry.ScriptPath,
			StateData: stateData, Risk: entry.Risk,
		})
	}
	if !report.Enabled {
		report.Entries = nil
	}
	return report, nil
}

func decodePartySkillCatalogDocument(data []byte) (partySkillCatalogDocument, error) {
	var raw partySkillCatalogDocument
	if err := foundationconfig.DecodeJSONBytes(data, &raw); err != nil {
		return partySkillCatalogDocument{}, err
	}
	if raw.Enabled == nil || raw.MaxSkillLevel == nil || raw.Skills == nil {
		return partySkillCatalogDocument{}, fmt.Errorf("party skill catalog requires enabled, max_skill_level, and skills")
	}
	if *raw.MaxSkillLevel < 1 || *raw.MaxSkillLevel > maxSafePartySkillLevel {
		return partySkillCatalogDocument{}, fmt.Errorf("party skill catalog max_skill_level must be between 1 and %d", maxSafePartySkillLevel)
	}
	return raw, nil
}

func decodePartySkillCatalogEntry(index int, data []byte) (partySkillCatalogEntry, error) {
	var raw partySkillCatalogEntryDocument
	if err := foundationconfig.DecodeJSONBytes(data, &raw); err != nil {
		return partySkillCatalogEntry{}, fmt.Errorf("party skill catalog entry %d: %w", index, err)
	}
	if raw.Job == nil || raw.SkillIndex == nil || raw.State == nil || raw.Level == nil {
		return partySkillCatalogEntry{}, fmt.Errorf("party skill catalog entry %d requires job, skill_index, state, and level", index)
	}
	var stateData json.RawMessage
	if raw.StateData != nil {
		stateData = append(json.RawMessage(nil), (*raw.StateData)...)
	}
	return partySkillCatalogEntry{
		Disabled: raw.Disabled, Job: *raw.Job, SkillIndex: *raw.SkillIndex,
		State: *raw.State, Level: *raw.Level, Name: raw.Name,
		ScriptPath: raw.ScriptPath, StateData: stateData, Risk: raw.Risk,
	}, nil
}

func SetPartySkillCatalogEnabled(path string, enabled bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw, err := decodePartySkillCatalogDocument(data)
	if err != nil {
		return err
	}
	report, err := parsePartySkillCatalog(data)
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return &PartySkillCatalogValidationError{Issues: report.Issues}
	}
	raw.Enabled = &enabled
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	report, err = parsePartySkillCatalog(out)
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return &PartySkillCatalogValidationError{Issues: report.Issues}
	}
	return atomicfile.WriteFile(path, out, 0644)
}

func (r *PartySkillCatalogReport) addPartySkillIssue(index int, entry partySkillCatalogEntry, reason string) {
	r.Issues = append(r.Issues, PartySkillCatalogIssue{
		Index: index, Job: entry.Job, SkillIndex: entry.SkillIndex, State: entry.State, Reason: reason,
	})
}

func invalidPartySkillEntryReason(entry partySkillCatalogEntry) string {
	switch {
	case entry.Job < 0 || entry.Job > 255:
		return fmt.Sprintf("job %d is outside 0..255", entry.Job)
	case entry.Level <= 0 || entry.Level > maxSafePartySkillLevel:
		return fmt.Sprintf("level %d is outside 1..%d", entry.Level, maxSafePartySkillLevel)
	case entry.SkillIndex <= 0 || entry.SkillIndex > 255:
		return fmt.Sprintf("skill_index %d is outside 1..255", entry.SkillIndex)
	case entry.State < 0 || entry.State > 255:
		return fmt.Sprintf("state %d is outside 0..255", entry.State)
	case entry.Risk < 0 || entry.Risk > 2:
		return fmt.Sprintf("risk %d is outside 0..2", entry.Risk)
	default:
		return ""
	}
}

func decodePartySkillStateData(raw json.RawMessage) ([]int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []int
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("invalid state_data: %w", err)
	}
	return values, nil
}

func partySkillStateData(values []int) ([]byte, error) {
	if len(values) > 3 {
		return nil, fmt.Errorf("state_data has %d values, maximum is 3", len(values))
	}
	data := make([]byte, 0, len(values)*3)
	for _, value := range values {
		if value < 0 || value > 0xffffff {
			return nil, fmt.Errorf("state_data value %d is outside 0..16777215", value)
		}
		data = append(data, byte(value), byte(value>>8), byte(value>>16))
	}
	return data, nil
}
