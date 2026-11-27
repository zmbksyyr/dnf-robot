package catalog

import (
	"bytes"
	"encoding/json"
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
  "enabled": true,
  "max_skill_level": 50,
  "skills": [
    {"enabled":true,"job":6,"skill_index":3,"state":22,"level":5,"name":"ok","state_data":[3],"risk":1},
    {"enabled":true,"job":6,"skill_index":4,"state":23,"level":60,"name":"too_high","state_data":[0],"risk":1},
    {"enabled":true,"job":6,"skill_index":5,"state":24,"level":10,"disabled":true,"state_data":[0],"risk":1},
    {"enabled":true,"job":2,"skill_index":6,"state":25,"level":10,"state_data":[0],"risk":1}
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
	if len(got) != 1 || got[0].SkillIndex != 3 || got[0].Level != 5 || got[0].Name != "ok" || !bytes.Equal(got[0].StateData, []byte{3}) {
		t.Fatalf("filtered catalog = %+v", got)
	}
}

func TestLoadPartySkillsRejectsInvalidStateData(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"enabled":true,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3,"level":1,"state_data":[2147483648]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := LoadPartySkills(dir); err == nil {
		t.Fatal("invalid state_data was accepted")
	}
}

func TestLoadPartySkillsRetainsPreviousSnapshotOnInvalidEntry(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
  "enabled": true,
  "max_skill_level": 70,
  "skills": [
    {"enabled":true,"job":6,"skill_index":3,"state":22,"level":5,"state_data":[3]},
      {"enabled":true,"job":6,"skill_index":4,"state":23,"level":10,"state_data":[2147483648]},
    {"enabled":true,"job":6,"skill_index":5,"state":24,"level":15,"state_data":"AQID"},
    {"enabled":true,"job":6,"skill_index":6,"state":25,"level":20,"state_data":[4,5]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	previous := []shared.PartySkillState{{Job: 6, SkillIndex: 99, State: 99, Level: 1}}
	shared.SetPartySkillStates(previous)
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
	if len(got) != 1 || got[0].SkillIndex != 99 {
		t.Fatalf("invalid file replaced previous snapshot: %+v", got)
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

func TestLoadPartySkillsRejectsOutOfRangeMaxSkillLevel(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
  "enabled": true,
  "max_skill_level": 85,
  "skills": [
    {"enabled":true,"job":2,"skill_index":6,"state":25,"level":70,"state_data":[0]},
    {"enabled":true,"job":2,"skill_index":7,"state":26,"level":71,"state_data":[0]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := LoadPartySkills(dir); err == nil {
		t.Fatal("out-of-range max_skill_level unexpectedly loaded")
	}
}

func TestLoadPartySkillsRequiresExplicitEnabled(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"max_skill_level":70,"skills":[{"enabled":true,"job":6,"skill_index":3,"state":22,"level":5,"state_data":[3]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := LoadPartySkills(dir); err == nil {
		t.Fatal("catalog without enabled unexpectedly loaded")
	}
}

func TestPartySkillCatalogRejectsUnknownMissingAndOutOfRangeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "party_skill_catalog.json")
	for _, data := range []string{
		`{"enabled":false,"max_skill_level":70,"skills":[],"legacy":true}`,
		`{"enabled":false,"skills":[]}`,
		`{"enabled":false,"max_skill_level":0,"skills":[]}`,
		`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3,"level":1,"legacy":true}]}`,
		`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3}]}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPartySkillCatalog(path); err == nil {
			t.Fatalf("invalid catalog unexpectedly decoded: %s", data)
		}
	}

	for _, data := range []string{
		`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3,"level":71}]}`,
		`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":1,"skill_index":2,"state":3,"level":1,"risk":3}]}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		report, err := ReadPartySkillCatalog(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Issues) != 1 {
			t.Fatalf("invalid disabled entry not rejected: report=%+v", report)
		}
	}
}

func TestReleasedRuntimeCatalogsUseCanonicalSchemas(t *testing.T) {
	defaults := filepath.Join("..", "..", "bootstrap", "runtime", "defaults")
	if _, err := ReadNameTemplates(filepath.Join(defaults, "robot_name_templates.json")); err != nil {
		t.Fatalf("released name template rejected: %v", err)
	}
	if _, err := ReadShoutTemplates(filepath.Join(defaults, "robot_shout_templates.json")); err != nil {
		t.Fatalf("released shout template rejected: %v", err)
	}
	report, err := ReadPartySkillCatalog(filepath.Join(defaults, "party_skill_catalog.json"))
	if err != nil {
		t.Fatalf("released party skill catalog rejected: %v", err)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("released party skill catalog issues: %v", (&PartySkillCatalogValidationError{Issues: report.Issues}).Error())
	}
}

func TestSetPartySkillCatalogEnabledUpdatesTopLevelFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "party_skill_catalog.json")
	data := []byte(`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":6,"skill_index":3,"state":22,"level":5,"state_data":[3]}]}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetPartySkillCatalogEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	report, err := ReadPartySkillCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Enabled || len(report.Entries) != 1 {
		t.Fatalf("enabled catalog = %+v", report)
	}
	if err := SetPartySkillCatalogEnabled(path, false); err != nil {
		t.Fatal(err)
	}
	report, err = ReadPartySkillCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Enabled || len(report.Entries) != 0 || report.SourceCount != 1 {
		t.Fatalf("disabled catalog = %+v", report)
	}
}

func TestSetPartySkillCatalogEnabledRejectsInvalidEnabledSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "party_skill_catalog.json")
	data := []byte(`{"enabled":false,"max_skill_level":70,"skills":[{"enabled":true,"job":6,"skill_index":0,"state":22,"level":5}]}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetPartySkillCatalogEnabled(path, true); err == nil {
		t.Fatal("invalid enabled snapshot unexpectedly saved")
	}
	report, err := ReadPartySkillCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Enabled {
		t.Fatal("failed enable changed the file")
	}
}

func TestSyncPartySkillCatalogBuildsSwitchesFromPVF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	pvfPath := filepath.Join(dir, "skill_state_catalog.json")
	initial := `{
  "enabled": true,
  "max_skill_level": 70,
  "skills": [
    {"enabled":true,"job":1,"skill_index":2,"state":3,"level":5,"name":"verified","script_path":"sqr/fighter/verified.nut","state_data":[3],"risk":1},
    {"enabled":true,"job":9,"skill_index":9,"state":9,"level":1,"name":"removed"}
  ]
}`
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
	if !report.Enabled || report.SourceCount != 2 || len(report.Entries) != 1 {
		t.Fatalf("synced report = %+v", report)
	}
	entry := report.Entries[0]
	if entry.Job != 1 || entry.SkillIndex != 2 || entry.Name != "verified" || entry.Level != 5 || entry.Risk != 1 || !bytes.Equal(entry.StateData, []byte{3}) {
		t.Fatalf("preserved entry = %+v", entry)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Skills []struct {
			ID       string `json:"id"`
			Enabled  bool   `json:"enabled"`
			JobLabel string `json:"job_label"`
			Name     string `json:"name"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Skills) != 2 {
		t.Fatalf("generated switches = %+v", document.Skills)
	}
	if document.Skills[0].ID == "" || document.Skills[0].JobLabel == "" {
		t.Fatalf("generated switch metadata missing = %+v", document.Skills)
	}
	if document.Skills[1].Enabled {
		t.Fatalf("new switch unexpectedly enabled = %+v", document.Skills[1])
	}
	changed, err = SyncPartySkillCatalog(path, pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second synchronization was not idempotent")
	}
}

func TestPartySkillCatalogMatchesCapturedAPCStateVectors(t *testing.T) {
	// Advanced Aganzo: the slash starts with 00 01; its later charged
	// substate carries 02 03 87 5d (substate 2, weapon 3, charge 989 ms).
	report, err := parsePartySkillCatalog([]byte(`{"enabled":true,"max_skill_level":70,"skills":[
		{"enabled":true,"job":0,"skill_index":9,"state":23,"level":1,"state_data":[0,1]},
		{"enabled":true,"job":0,"skill_index":9,"state":249,"level":1,"state_data":[2,3,989]}
	]}`))
	if err != nil || len(report.Issues) != 0 || len(report.Entries) != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	for i, want := range [][]byte{{0, 1}, {2, 3, 0x87, 0x5d}} {
		if !bytes.Equal(report.Entries[i].StateData, want) {
			t.Fatalf("entry %d data=%x want captured %x", i, report.Entries[i].StateData, want)
		}
	}
}

func TestPartySkillCatalogRejectsEmptyEnabledSwordmanSlash(t *testing.T) {
	report, err := parsePartySkillCatalog([]byte(`{"enabled":true,"max_skill_level":70,"skills":[
		{"enabled":true,"job":0,"skill_index":9,"state":23,"level":1,"state_data":[]},
		{"enabled":false,"job":0,"skill_index":9,"state":23,"level":1,"state_data":[]},
		{"enabled":true,"job":8,"skill_index":1,"state":23,"level":1,"state_data":[]}
	]}`))
	if err != nil || len(report.Issues) != 1 || len(report.Entries) != 1 || report.SwitchOffCount != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if !strings.Contains(report.Issues[0].Reason, "requires non-empty state_data") {
		t.Fatalf("missing actionable issue: %+v", report.Issues)
	}
}

func TestPartySkillStateDataBase128Boundaries(t *testing.T) {
	for _, tc := range []struct {
		values []int
		want   []byte
	}{
		{[]int{0, 127, 128}, []byte{0, 0x7f, 0x81, 0}},
		{[]int{16383, 16384, 0xffffff}, []byte{0xff, 0x7f, 0x81, 0x80, 0, 0x87, 0xff, 0xff, 0x7f}},
		// The captured shining cut carries three integers, including two zeros.
		{[]int{3, 0, 0}, []byte{3, 0, 0}},
	} {
		got, err := partySkillStateData(tc.values)
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Fatalf("values=%v got=%x want=%x err=%v", tc.values, got, tc.want, err)
		}
	}
}

func TestPartySkillStateDataMatchesNativeInt32Vectors(t *testing.T) {
	// Original 0x879c30/0x8759c0 fully encode and decode these vectors in
	// private frozen memory. Negative ints use their uint32 bit pattern.
	for _, tc := range []struct {
		values []int
		want   []byte
	}{
		{[]int{0, 127, 128, 16383, 16384, 0xffffff, 0x1000000}, []byte{0, 0x7f, 0x81, 0, 0xff, 0x7f, 0x81, 0x80, 0, 0x87, 0xff, 0xff, 0x7f, 0x88, 0x80, 0x80, 0}},
		{[]int{0x7fffffff, -1, -0x80000000, -127}, []byte{0x87, 0xff, 0xff, 0xff, 0x7f, 0x8f, 0xff, 0xff, 0xff, 0x7f, 0x88, 0x80, 0x80, 0x80, 0, 0x8f, 0xff, 0xff, 0xff, 1}},
	} {
		got, err := partySkillStateData(tc.values)
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Fatalf("values=%v got=%x want=%x err=%v", tc.values, got, tc.want, err)
		}
	}
}

func TestNativeInt32VectorSurvivesCatalogPublication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	if err := os.WriteFile(path, []byte(`{"enabled":true,"mode":"auto","max_skill_level":70,"skills":[
		{"enabled":true,"job":8,"skill_index":2,"state":25,"level":1,"script_path":"sqr/test.nut","state_data":[2147483647,-1,-2147483648,-127]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadPartySkills(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shared.SetPartySkillStates(nil) })
	entries := shared.PartySkillStatesForJob(8)
	want := []byte{0x87, 0xff, 0xff, 0xff, 0x7f, 0x8f, 0xff, 0xff, 0xff, 0x7f, 0x88, 0x80, 0x80, 0x80, 0, 0x8f, 0xff, 0xff, 0xff, 1}
	if len(entries) != 1 || entries[0].SkillIndex != 2 || entries[0].State != 25 || !bytes.Equal(entries[0].StateData, want) {
		t.Fatalf("native vector changed at catalog publication: %+v", entries)
	}
}

func TestPartySkillStateDataRespectsWireLength(t *testing.T) {
	values := make([]int, 65535)
	got, err := partySkillStateData(values)
	if err != nil || len(got) != 65535 {
		t.Fatalf("word-sized vector: bytes=%d err=%v", len(got), err)
	}
	if _, err := partySkillStateData(append(values, 0)); err == nil {
		t.Fatal("unrepresentable vector was accepted")
	}
	for _, value := range []int64{0x80000000, -0x80000001} {
		// On a 32-bit host JSON already rejects these before encoding.
		if int64(int(value)) == value {
			if _, err := partySkillStateData([]int{int(value)}); err == nil {
				t.Fatal("out-of-range int32 was accepted")
			}
		}
	}
	// Count alone cannot bound wire size: each negative int needs five bytes.
	values = make([]int, 13108)
	for i := range values {
		values[i] = -1
	}
	if _, err := partySkillStateData(values); err == nil {
		t.Fatal("encoded byte length overflow was accepted")
	}
}

func TestSyncPartySkillCatalogFillsOnlyMissingOriginalParameters(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        []byte
	}{
		{"missing", "", []byte{5}},
		{"null", `,"state_data":null`, []byte{5}},
		{"explicit empty", `,"state_data":[]`, []byte{}},
		{"explicit vector", `,"state_data":[9]`, []byte{9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
			config := `{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[{"enabled":true,"job":8,"skill_index":2,"state":25,"level":1,"script_path":"sqr/skill.nut"` + tc.field + `}]}`
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pvfPath, []byte(`[{"job":8,"skill_index":2,"state":25,"script_path":"sqr/skill.nut","native_state_data":[5]}]`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := SyncPartySkillCatalog(path, pvfPath); err != nil {
				t.Fatal(err)
			}
			report, err := ReadPartySkillCatalog(path)
			if err != nil || len(report.Issues) != 0 || len(report.Entries) != 1 || !bytes.Equal(report.Entries[0].StateData, tc.want) {
				t.Fatalf("report=%+v err=%v want=%x", report, err, tc.want)
			}
			if changed, err := SyncPartySkillCatalog(path, pvfPath); err != nil || changed {
				t.Fatalf("unchanged parameters rewrote catalog: changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestSyncPartySkillCatalogPreservesKnownEmptyPVFVector(t *testing.T) {
	dir := t.TempDir()
	path, pvfPath := filepath.Join(dir, "party.json"), filepath.Join(dir, "pvf.json")
	if err := os.WriteFile(path, []byte(`{"mode":"auto","enabled":true,"max_skill_level":70,"skills":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pvfPath, []byte(`[{"job":8,"skill_index":1,"state":20,"script_path":"sqr/character/atmage/windstrike/wind_strike.nut","native_state_data":[]}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncPartySkillCatalog(path, pvfPath); err != nil {
		t.Fatal(err)
	}
	report, err := ReadPartySkillCatalog(path)
	if err != nil || report.UnknownStateDataCount != 0 || len(report.Entries) != 1 || report.Entries[0].StateData == nil || len(report.Entries[0].StateData) != 0 {
		t.Fatalf("known empty vector became unknown: report=%+v err=%v", report, err)
	}
	var document struct {
		Skills []struct {
			StateData json.RawMessage `json:"state_data"`
		} `json:"skills"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Skills) != 1 || string(document.Skills[0].StateData) != "[]" {
		t.Fatalf("serialized state_data = %+v", document.Skills)
	}
}

func TestSyncPartySkillCatalogRejectsEntriesWithoutSwitches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	pvfPath := filepath.Join(dir, "skill_state_catalog.json")
	if err := os.WriteFile(path, []byte(`{"enabled":true,"max_skill_level":70,"skills":[{"job":1,"skill_index":2,"state":3,"level":1}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pvfPath, []byte(`[{"job":1,"skill_index":2,"state":3,"script_path":"sqr/fighter/skill.nut"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncPartySkillCatalog(path, pvfPath); err == nil {
		t.Fatal("entry without enabled switch was accepted")
	}
}
