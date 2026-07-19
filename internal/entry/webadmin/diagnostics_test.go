package webadmin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsSectionStatusUsesWorstCheck(t *testing.T) {
	b := diagnosticsBuilder{}
	b.addSection("mixed",
		diagnosticsCheck{Name: "a", Status: diagOK},
		diagnosticsCheck{Name: "b", Status: diagWarn},
	)
	if got := b.report.Sections[0].Status; got != diagWarn {
		t.Fatalf("section status = %s, want warn", got)
	}
	b.addSection("bad",
		diagnosticsCheck{Name: "a", Status: diagWarn},
		diagnosticsCheck{Name: "b", Status: diagError},
	)
	if got := b.report.Sections[1].Status; got != diagError {
		t.Fatalf("section status = %s, want error", got)
	}
}

func TestCompareFileHashCheckDetectsMismatch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.dat")
	b := filepath.Join(dir, "b.dat")
	if err := os.WriteFile(a, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := compareFileHashCheck("same", a, b).Status; got != diagOK {
		t.Fatalf("same status = %s, want ok", got)
	}
	if err := os.WriteFile(b, []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := compareFileHashCheck("different", a, b).Status; got != diagError {
		t.Fatalf("different status = %s, want error", got)
	}
}

func TestSkillCatalogCheckReportsWhitelistRisks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "party_skill_catalog.json")
	raw := `{"max_skill_level":70,"skills":[{"job":1,"skill_index":2,"state":3,"level":71,"state_data":[256]}]}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	check := skillDiagnosticsChecks(dir)[0]
	if check.Status != diagWarn {
		t.Fatalf("status = %s, want warn", check.Status)
	}
}

func TestSkillCatalogCheckAcceptsPVFStateDataBase64(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvf_skill_state_catalog.json")
	raw := `[{"job":1,"skill_index":2,"state":3,"level":70,"state_data":"AQID"}]`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	_, check, _ := pvfSkillCatalogCheck(path)
	if check.Status != diagOK {
		t.Fatalf("status = %s message=%s, want ok", check.Status, check.Message)
	}
}

func TestSkillDiagnosticsReportsEffectiveCandidatesByJob(t *testing.T) {
	dir := t.TempDir()
	whitelist := `{
  "max_skill_level":70,
  "skills":[
    {"job":1,"skill_index":2,"state":3,"level":10,"script_path":"SQR\\fighter\\a.nut","state_data":[1]},
    {"job":1,"skill_index":4,"state":5,"level":20,"state_data":[2]},
    {"job":2,"skill_index":6,"state":7,"level":30,"state_data":[3]}
  ]
}`
	pvf := `[
  {"job":1,"skill_index":2,"state":3,"script_path":"/sqr/fighter/A.NUT/"},
  {"job":1,"skill_index":4,"state":5},
  {"job":2,"skill_index":6,"state":7}
]`
	writeSkillDiagnosticCatalogs(t, dir, whitelist, pvf)

	checks := skillDiagnosticsChecks(dir)
	effective := checks[2]
	if effective.Status != diagOK {
		t.Fatalf("status = %s message=%s", effective.Status, effective.Message)
	}
	observed := effective.Observed.(map[string]interface{})
	if observed["total"] != 3 {
		t.Fatalf("total = %v", observed["total"])
	}
	byJob := observed["by_job"].(map[int]int)
	if byJob[1] != 2 || byJob[2] != 1 {
		t.Fatalf("by_job = %v", byJob)
	}
}

func TestSkillDiagnosticsWarnsButKeepsValidWhitelistEntries(t *testing.T) {
	dir := t.TempDir()
	whitelist := `{
  "max_skill_level":70,
  "skills":[
    {"job":6,"skill_index":3,"state":22,"level":5,"state_data":[3]},
    {"job":6,"skill_index":4,"state":23,"level":10,"state_data":"AQID"}
  ]
}`
	pvf := `[{"job":6,"skill_index":3,"state":22}]`
	writeSkillDiagnosticCatalogs(t, dir, whitelist, pvf)

	checks := skillDiagnosticsChecks(dir)
	if checks[0].Status != diagWarn {
		t.Fatalf("whitelist status = %s, want warn", checks[0].Status)
	}
	if checks[2].Status != diagWarn {
		t.Fatalf("effective status = %s, want warn", checks[2].Status)
	}
	observed := checks[2].Observed.(map[string]interface{})
	if observed["total"] != 1 || observed["whitelist_invalid"] != 1 {
		t.Fatalf("observed = %#v", observed)
	}
}

func TestSkillDiagnosticsErrorsWhenCatalogsHaveNoIntersection(t *testing.T) {
	dir := t.TempDir()
	whitelist := `{"max_skill_level":70,"skills":[{"job":1,"skill_index":2,"state":3,"level":10}]}`
	pvf := `[{"job":1,"skill_index":9,"state":9}]`
	writeSkillDiagnosticCatalogs(t, dir, whitelist, pvf)

	effective := skillDiagnosticsChecks(dir)[2]
	if effective.Status != diagError {
		t.Fatalf("status = %s message=%s, want error", effective.Status, effective.Message)
	}
	observed := effective.Observed.(map[string]interface{})
	if observed["total"] != 0 || observed["missing_pvf"] != 1 {
		t.Fatalf("observed = %#v", observed)
	}
}

func TestPartySkillErrorPatternsMatchRuntimeLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log_robot")
	logText := "[PARTY_DUNGEON_SKILL_PROFILE_ERROR]\n[PARTY_DUNGEON_SKILL_CAST_ERROR]\n[PARTY_DUNGEON_SKILL_RECOVER_ERROR]\n"
	if err := os.WriteFile(path, []byte(logText), 0644); err != nil {
		t.Fatal(err)
	}
	check := recentLogPatternCheck("skills", path, partySkillErrorLogPatterns)
	if check.Status != diagWarn {
		t.Fatalf("status = %s, want warn", check.Status)
	}
	hits := check.Observed.(map[string]int)
	for _, pattern := range partySkillErrorLogPatterns {
		if hits[pattern] != 1 {
			t.Fatalf("hits = %v, missing %s", hits, pattern)
		}
	}
}

func writeSkillDiagnosticCatalogs(t *testing.T, dir, whitelist, pvf string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "party_skill_catalog.json"), []byte(whitelist), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pvf_skill_state_catalog.json"), []byte(pvf), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPartyAccountRangeCheckUsesConfiguredRobotRange(t *testing.T) {
	dir := t.TempDir()
	raw := "[create]\nrobot_uid_start = 18000000\nrobot_uid_end = 18001999\n"
	if err := os.WriteFile(filepath.Join(dir, "robot_config.ini"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		patchStart uint32
		patchEnd   uint32
		wantStatus string
	}{
		{name: "covered", patchStart: 18000000, patchEnd: 18001000, wantStatus: diagOK},
		{name: "start too high", patchStart: 18000001, patchEnd: 18001000, wantStatus: diagError},
		{name: "exclusive end too low", patchStart: 18000000, patchEnd: 18000999, wantStatus: diagError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := partyAccountRangeCheck(dir, tt.patchStart, tt.patchEnd)
			if check.Status != tt.wantStatus {
				t.Fatalf("status = %s message=%s, want %s", check.Status, check.Message, tt.wantStatus)
			}
		})
	}
}

func TestPartyAccountRangeCheckReportsConfigLoadFailure(t *testing.T) {
	check := partyAccountRangeCheck(t.TempDir(), 17000000, 17001000)
	if check.Status != diagError {
		t.Fatalf("status = %s message=%s, want error", check.Status, check.Message)
	}
}
