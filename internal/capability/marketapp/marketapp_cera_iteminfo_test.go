package marketapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeNativeCeraItemInfoRowsReplacesPlaceholderAndIsIdempotent(t *testing.T) {
	existing := "1001 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n" +
		"2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `item_2675336` `name2_2675336` 13002\n"
	rows := []ceraRow{
		{ItemID: 2675347, Enabled: true},
		{ItemID: 2675336, Enabled: true},
		{ItemID: 2675347},
		{},
	}

	donors := map[uint32][]byte{
		2675336: []byte("2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `native100` `native100` 13002"),
		2675347: []byte("2675347 2 1 1 1 1 1 1 1 1 1 1 1 1 `native3000` `native3000` 13002"),
	}
	got, changed, err := mergeNativeCeraItemInfoRows([]byte(existing), rows, donors)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("placeholder rows should be replaced")
	}
	if strings.Contains(string(got), "`item_2675336`") {
		t.Fatalf("placeholder cera row was preserved: %q", got)
	}
	for id, line := range donors {
		if strings.Count(string(got), string(line)) != 1 {
			t.Fatalf("native row %d count != 1: %q", id, got)
		}
	}
	again, changed, err := mergeNativeCeraItemInfoRows(got, rows, donors)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(again, got) {
		t.Fatalf("second merge changed data: changed=%v\nfirst=%q\nsecond=%q", changed, got, again)
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
	native := base +
		"2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `native100` `native100` 13002\n" +
		"2675347 2 1 1 1 1 1 1 1 1 1 1 1 1 `native3000` `native3000` 13002\n"
	mustWriteText(t, source, base)
	mustWriteText(t, target, native)

	app := testApp(t)
	app.cfg.ItemInfoSourcePath = source
	app.cfg.ItemInfoTargets = []string{target, missing}
	app.cfg.Cera.Items = []ceraRow{{ItemID: 2675336, Enabled: true}, {ItemID: 2675347}}

	status := app.ensureConfiguredCeraItemInfo()
	if status.Error != "" || status.Synced != 1 || status.Skipped != 2 {
		t.Fatalf("unexpected first status: %#v", status)
	}
	for _, path := range []string{source, target} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"2675336", "2675347"} {
			if !strings.Contains(string(data), id+" 2 ") || strings.Contains(string(data), "`item_"+id+"`") {
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

func TestMergeNativeCeraItemInfoRowsRejectsInvalidBase(t *testing.T) {
	donors := map[uint32][]byte{2675336: []byte("2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `native` `native` 13002")}
	if _, _, err := mergeNativeCeraItemInfoRows([]byte("bad iteminfo\n"), []ceraRow{{ItemID: 2675336}}, donors); err == nil {
		t.Fatal("invalid iteminfo should fail")
	}
}

func TestLoadNativeCeraItemInfoRowsRejectsGeneratedPlaceholders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iteminfo.dat")
	mustWriteText(t, path, "2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `item_2675336` `name2_2675336` 13002\n")
	if _, err := loadNativeCeraItemInfoRows([]string{path}, []ceraRow{{ItemID: 2675336}}); err == nil {
		t.Fatal("generated placeholder should not be accepted as native metadata")
	}
}

func TestLoadNativeCeraItemInfoRowsAcceptsNativeNamesWithSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iteminfo.dat")
	mustWriteText(t, path, "2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `100 gold package` `100 gold package` 13002\n")
	rows, err := loadNativeCeraItemInfoRows([]string{path}, []ceraRow{{ItemID: 2675336}})
	if err != nil || rows[2675336] == nil {
		t.Fatalf("native row with spaces was rejected: rows=%q err=%v", rows, err)
	}
}

func TestMergeNativeItemInfoRowsPreservesServiceOnlyItems(t *testing.T) {
	data := []byte("2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `item_2675336` `name2_2675336` 13002\r\n")
	donors := map[uint32][]byte{
		2675336: []byte("2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `native_gold` `native_gold` 13002"),
		2681762: []byte("2681762 2 1 1 1 1 1 1 1 1 1 1 1 1 `native_point` `native_point` 13002"),
	}
	got, changed := mergeNativeItemInfoRows(data, donors, map[uint32]bool{2675336: true})
	if !changed {
		t.Fatal("native rows should be merged")
	}
	text := string(got)
	for _, want := range []string{"`native_gold`", "2681762 ", "`native_point`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged iteminfo missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "`item_2675336`") {
		t.Fatalf("gold placeholder was not replaced: %q", text)
	}
}
