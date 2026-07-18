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
