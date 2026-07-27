package marketapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendMissingCeraItemInfoRowsPreservesExistingAndIsIdempotent(t *testing.T) {
	existing := "1001 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n" +
		"2675336 9 1 1 1 1 1 1 1 1 1 1 1 1 `keep` `keep2` 999\n"
	rows := []ceraRow{
		{ItemID: 2675347, Enabled: true},
		{ItemID: 2675336, Enabled: true},
		{ItemID: 2675347},
		{},
	}

	got, added, err := appendMissingCeraItemInfoRows([]byte(existing), rows)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	if !strings.Contains(string(got), "2675336 9 ") {
		t.Fatalf("existing cera row was replaced: %q", got)
	}
	line := ceraItemInfoLine(2675347)
	if strings.Count(string(got), line) != 1 {
		t.Fatalf("generated row count != 1: %q", got)
	}
	fields := strings.Fields(line)
	if len(fields) != 17 {
		t.Fatalf("generated field count = %d, want 17: %q", len(fields), line)
	}
	if fields[1] != "2" || fields[14] != "`item_2675347`" || fields[15] != "`name2_2675347`" || fields[16] != "13002" {
		t.Fatalf("unexpected generated row: %#v", fields)
	}

	again, added, err := appendMissingCeraItemInfoRows(got, rows)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || !bytes.Equal(again, got) {
		t.Fatalf("second merge changed data: added=%d\nfirst=%q\nsecond=%q", added, got, again)
	}
}

func TestEnsureConfiguredCeraItemInfoUpdatesExistingFilesOnly(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "pvf_iteminfo.dat")
	target := filepath.Join(dir, "point", "iteminfo.dat")
	missing := filepath.Join(dir, "missing", "iteminfo.dat")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	base := "1001 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n"
	mustWriteText(t, source, base)
	mustWriteText(t, target, base)

	app := testApp(t)
	app.cfg.ItemInfoSourcePath = source
	app.cfg.ItemInfoTargets = []string{target, missing}
	app.cfg.Cera.Items = []ceraRow{{ItemID: 2675336, Enabled: true}, {ItemID: 2675347}}

	status := app.ensureConfiguredCeraItemInfo()
	if status.Error != "" || status.Synced != 2 || status.Skipped != 1 {
		t.Fatalf("unexpected first status: %#v", status)
	}
	for _, path := range []string{source, target} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"2675336", "2675347"} {
			if !strings.Contains(string(data), id+" 2 ") {
				t.Fatalf("%s missing cera row %s: %q", path, id, data)
			}
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing target should not be created: %v", err)
	}

	status = app.ensureConfiguredCeraItemInfo()
	if status.Error != "" || status.Synced != 0 || status.Skipped != 3 {
		t.Fatalf("unexpected idempotent status: %#v", status)
	}
}

func TestAppendMissingCeraItemInfoRowsRejectsInvalidBase(t *testing.T) {
	if _, _, err := appendMissingCeraItemInfoRows([]byte("bad iteminfo\n"), []ceraRow{{ItemID: 2675336}}); err == nil {
		t.Fatal("invalid iteminfo should fail")
	}
}
