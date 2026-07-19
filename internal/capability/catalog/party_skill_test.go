package catalog

import (
	"bytes"
	"os"
	"path/filepath"
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
