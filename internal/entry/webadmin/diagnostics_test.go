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
	path := filepath.Join(t.TempDir(), "party_skill_catalog.json")
	raw := `{"max_skill_level":70,"skills":[{"job":1,"skill_index":2,"state":3,"level":71,"state_data":[256]}]}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	check := skillCatalogCheck(path, true)
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
	check := skillCatalogCheck(path, false)
	if check.Status != diagOK {
		t.Fatalf("status = %s message=%s, want ok", check.Status, check.Message)
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
