package catalog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"robot/internal/shared"
)

func TestLoadPartySkillsIndexesFilteredCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	data := []byte(`{
  "max_skill_level": 70,
  "skills": [
    {"job":6,"skill_index":3,"state":22,"level":5,"name":"ok","state_data":[3],"risk":1},
    {"job":6,"skill_index":4,"state":23,"level":75,"name":"too_high","state_data":[0],"risk":1},
    {"job":6,"skill_index":5,"state":24,"level":10,"disabled":true,"state_data":[0],"risk":1},
    {"job":2,"skill_index":6,"state":25,"level":10,"state_data":[0],"risk":1}
  ]
}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	previous := shared.PartySkillStatesForJob(6)
	t.Cleanup(func() { shared.SetPartySkillStates(previous) })

	if err := LoadPartySkills(dir); err != nil {
		t.Fatal(err)
	}
	got := shared.PartySkillStatesForJob(6)
	if len(got) != 1 || got[0].SkillIndex != 3 || got[0].Level != 5 || got[0].Name != "ok" || !bytes.Equal(got[0].StateData, []byte{3, 0, 0}) {
		t.Fatalf("filtered catalog = %+v", got)
	}
}

func TestLoadPartySkillsRejectsInvalidStateData(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"skills":[{"job":1,"skill_index":2,"state":3,"level":1,"state_data":[16777216]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := LoadPartySkills(dir); err == nil {
		t.Fatal("invalid state_data was accepted")
	}
}

func TestLoadPartySkillsPublishesValidEntriesAlongsideInvalidStateData(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
  "skills": [
    {"job":6,"skill_index":3,"state":22,"level":5,"state_data":[3]},
    {"job":6,"skill_index":4,"state":23,"level":10,"state_data":[16777216]},
    {"job":6,"skill_index":5,"state":24,"level":15,"state_data":"AQID"},
    {"job":6,"skill_index":6,"state":25,"level":20,"state_data":[4,5]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shared.SetPartySkillStates(nil) })

	err := LoadPartySkills(dir)
	var validationErr *PartySkillCatalogValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if len(validationErr.Issues) != 2 || !strings.Contains(err.Error(), "2 invalid entries") {
		t.Fatalf("validation error = %v", err)
	}
	got := shared.PartySkillStatesForJob(6)
	if len(got) != 2 || got[0].SkillIndex != 3 || got[1].SkillIndex != 6 {
		t.Fatalf("published entries = %+v", got)
	}
	if !bytes.Equal(got[1].StateData, []byte{4, 0, 0, 5, 0, 0}) {
		t.Fatalf("second state data = %v", got[1].StateData)
	}
}

func TestPartySkillValidationErrorSummarizesMultipleIssues(t *testing.T) {
	err := (&PartySkillCatalogValidationError{Issues: []PartySkillCatalogIssue{
		{Index: 0, Job: 1, SkillIndex: 2, State: 3, Reason: "first"},
		{Index: 1, Job: 2, SkillIndex: 3, State: 4, Reason: "second"},
		{Index: 2, Job: 3, SkillIndex: 4, State: 5, Reason: "third"},
		{Index: 3, Job: 4, SkillIndex: 5, State: 6, Reason: "fourth"},
	}}).Error()
	for _, want := range []string{"4 invalid entries", "first", "second", "third", "and 1 more"} {
		if !strings.Contains(err, want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err, "fourth") {
		t.Fatalf("error was not bounded: %q", err)
	}
}

func TestLoadPartySkillsCannotRaiseSafeLevelLimit(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
  "max_skill_level": 85,
  "skills": [
    {"job":2,"skill_index":6,"state":25,"level":70,"state_data":[0]},
    {"job":2,"skill_index":7,"state":26,"level":71,"state_data":[0]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	previous := shared.PartySkillStatesForJob(2)
	t.Cleanup(func() { shared.SetPartySkillStates(previous) })

	if err := LoadPartySkills(dir); err != nil {
		t.Fatal(err)
	}
	got := shared.PartySkillStatesForJob(2)
	if len(got) != 1 || got[0].SkillIndex != 6 || got[0].Level != maxSafePartySkillLevel {
		t.Fatalf("safe-level catalog = %+v", got)
	}
}
