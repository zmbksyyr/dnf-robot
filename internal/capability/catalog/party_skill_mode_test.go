package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoModeSelectsSkillsWithoutPerSkillSwitches(t *testing.T) {
	report, err := parsePartySkillCatalog([]byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[
		{"enabled":false,"job":2,"skill_index":5,"state":30,"level":10,"state_data":[0],"risk":2},
		{"enabled":false,"job":2,"skill_index":6,"state":31,"level":20,"disabled":true,"state_data":[0],"risk":2},
		{"enabled":false,"job":2,"skill_index":7,"state":32,"level":30,"state_data":[],"risk":1}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != partySkillModeAuto || !report.Enabled {
		t.Fatalf("mode=%q enabled=%t", report.Mode, report.Enabled)
	}
	if report.SwitchOffCount != 0 || report.DisabledCount != 1 {
		t.Fatalf("switch_off=%d disabled=%d", report.SwitchOffCount, report.DisabledCount)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("auto entries = %+v", report.Entries)
	}
	for _, entry := range report.Entries {
		if entry.SkillIndex == 6 {
			t.Fatalf("explicitly disabled entry was selected: %+v", entry)
		}
	}
	// Selection is driven by learned skill and PVF matching later; the catalog
	// itself must already expose the entries the top-level switch enables.
	if got := len(report.Entries); got != report.SourceCount-1 {
		t.Fatalf("auto selected %d of %d source entries", got, report.SourceCount)
	}
}

func TestManualModeKeepsPerSkillSwitches(t *testing.T) {
	for name, body := range map[string]string{
		"absent": `{"enabled":true,"max_skill_level":70,"skills":[
			{"enabled":false,"job":2,"skill_index":5,"state":30,"level":10,"state_data":[0]},
			{"enabled":true,"job":2,"skill_index":6,"state":31,"level":20,"state_data":[0]}
		]}`,
		"manual": `{"mode":"manual","enabled":true,"max_skill_level":70,"skills":[
			{"enabled":false,"job":2,"skill_index":5,"state":30,"level":10,"state_data":[0]},
			{"enabled":true,"job":2,"skill_index":6,"state":31,"level":20,"state_data":[0]}
		]}`,
	} {
		report, err := parsePartySkillCatalog([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if report.Mode != partySkillModeManual {
			t.Fatalf("%s: mode=%q", name, report.Mode)
		}
		if report.SwitchOffCount != 1 || len(report.Entries) != 1 || report.Entries[0].SkillIndex != 6 {
			t.Fatalf("%s: entries=%+v switch_off=%d", name, report.Entries, report.SwitchOffCount)
		}
	}
}

func TestPartySkillModeRejectsUnknownValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "party_skill_catalog.json")
	data := `{"mode":"automatic","enabled":true,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3,"level":1}]}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPartySkillCatalog(path); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("unknown mode error = %v", err)
	}
}

func TestSyncPartySkillCatalogPreservesAutoMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	pvfPath := filepath.Join(dir, "skill_state_catalog.json")
	initial := `{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[
		{"enabled":false,"job":1,"skill_index":2,"state":3,"level":5,"name":"verified","script_path":"sqr/fighter/verified.nut","state_data":[3],"risk":1}
	]}`
	pvf := `[
		{"job":1,"skill_index":2,"state":3,"script_path":"sqr/fighter/verified.nut"},
		{"job":2,"skill_index":4,"state":5,"script_path":"sqr/gunner/newskill.nut"}
	]`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pvfPath, []byte(pvf), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := SyncPartySkillCatalog(path, pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("synchronization did not update the catalog")
	}
	report, err := ReadPartySkillCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != partySkillModeAuto || len(report.Entries) != 2 {
		t.Fatalf("synced report = %+v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Mode   string `json:"mode"`
		Skills []struct {
			Enabled bool `json:"enabled"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Mode != partySkillModeAuto {
		t.Fatalf("mode was not written: %q", document.Mode)
	}
	if !document.Skills[1].Enabled {
		t.Fatalf("new switch is off in auto mode: %+v", document.Skills)
	}
}

func TestReleasedPartySkillCatalogDefaultsToAuto(t *testing.T) {
	path := filepath.Join("..", "..", "bootstrap", "runtime", "defaults", "party_skill_catalog.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ReadPartySkillCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != partySkillModeAuto {
		t.Fatalf("released catalog mode = %q", report.Mode)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("released catalog issues = %+v", report.Issues)
	}
	// The shipped switch stays off, but turning it on must expose every entry
	// without the user enabling the individual skills.
	if report.Enabled || len(report.Entries) != 0 || report.SourceCount == 0 {
		t.Fatalf("released catalog enabled=%t entries=%d source=%d", report.Enabled, len(report.Entries), report.SourceCount)
	}
	enabled, err := parsePartySkillCatalog([]byte(strings.Replace(string(data), `"enabled": false,`, `"enabled": true,`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled || len(enabled.Entries) != report.SourceCount {
		t.Fatalf("enabled auto catalog entries=%d source=%d", len(enabled.Entries), report.SourceCount)
	}
	for _, entry := range enabled.Entries {
		if entry.SkillIndex <= 0 || entry.State < 0 {
			t.Fatalf("invalid auto entry = %+v", entry)
		}
	}
}
