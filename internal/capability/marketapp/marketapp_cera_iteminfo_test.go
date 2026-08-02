package marketapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeItemInfoOverlayUsesPVFAndRetainsOriginalOnlyRows(t *testing.T) {
	pvf := []byte("1001 0 `pvf_only`\r\n2675336 2 `pvf_gold`\r\n")
	original := map[uint32][]byte{
		2675336: []byte("2675336 2 `native_gold`"),
		2681762: []byte("2681762 2 `native_point`"),
	}

	got, changed := mergeItemInfoOverlay(pvf, original)
	if !changed {
		t.Fatal("original-only row should be merged")
	}
	text := string(got)
	for _, want := range []string{"1001 0 `pvf_only`", "2675336 2 `pvf_gold`", "2681762 2 `native_point`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged iteminfo missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "`native_gold`") {
		t.Fatalf("native row replaced PVF row: %q", text)
	}

	again, changed := mergeItemInfoOverlay(got, original)
	if changed || !bytes.Equal(again, got) {
		t.Fatalf("second merge changed data: changed=%v\nfirst=%q\nsecond=%q", changed, got, again)
	}
}

func TestValidateConfiguredCeraItemInfoRequiresOnlyEnabledStock(t *testing.T) {
	data := []byte("2675336 2 `gold`\n")
	rows := []ceraRow{
		{ItemID: 2675336, Enabled: true, RestockQty: 20},
		{ItemID: 2675337, Enabled: false, RestockQty: 20},
		{ItemID: 2675338, Enabled: true, RestockQty: 0},
	}
	if err := validateConfiguredCeraItemInfo(data, rows); err != nil {
		t.Fatal(err)
	}

	rows = append(rows, ceraRow{ItemID: 2675339, Enabled: true, RestockQty: 1})
	err := validateConfiguredCeraItemInfo(data, rows)
	if err == nil || !strings.Contains(err.Error(), "2675339") {
		t.Fatalf("missing enabled item was not reported: %v", err)
	}
}

func TestMissingConfiguredCeraItemIsNotGenerated(t *testing.T) {
	pvf := []byte("1001 0 `item`\n")
	got, changed := mergeItemInfoOverlay(pvf, nil)
	if changed || !bytes.Equal(got, pvf) {
		t.Fatalf("merge unexpectedly changed data: changed=%v data=%q", changed, got)
	}
	err := validateConfiguredCeraItemInfo(got, []ceraRow{{ItemID: 2675336, Enabled: true, RestockQty: 1}})
	if err == nil {
		t.Fatal("missing configured item should fail validation")
	}
	if strings.Contains(string(got), "2675336") {
		t.Fatalf("missing configured item was generated: %q", got)
	}
}

func TestEnsureConfiguredCeraItemInfoValidatesServiceTargetsBeforeSource(t *testing.T) {
	app := testApp(t)
	source := appPaths(app).PVFItemInfo()
	target := filepath.Join(app.configDir, "point", "iteminfo.dat")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	mustWriteText(t, source, "1001 0 `pvf_only`\n")
	mustWriteText(t, target, "2675336 2 `native_gold`\n")

	app.cfg.ItemInfoTargets = []string{target}
	app.cfg.Cera.Items = []ceraRow{{ItemID: 2675336, Enabled: true, RestockQty: 1}}

	status := app.ensureConfiguredCeraItemInfo()
	if status.Error != "" {
		t.Fatalf("valid target should not be rejected because source lacks native-only row: %#v", status)
	}
	sourceData, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sourceData), "2675336") {
		t.Fatalf("validation mutated source: %q", sourceData)
	}
}

func TestSyncItemInfoDATOverlaysPVFOnOriginalTarget(t *testing.T) {
	app := testApp(t)
	source := appPaths(app).PVFItemInfo()
	target := filepath.Join(app.configDir, "point", "iteminfo.dat")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	mustWriteText(t, source, "1001 0 `pvf_only`\n2675336 2 `pvf_gold`\n")
	mustWriteText(t, target, "2675336 2 `native_gold`\n2681762 2 `native_point`\n")

	app.cfg.ItemInfoTargets = []string{target}
	app.cfg.Cera.Items = []ceraRow{{ItemID: 2675336, Enabled: true, RestockQty: 1}}

	status := app.syncItemInfoDAT()
	if status.Error != "" || status.Synced != 1 {
		t.Fatalf("unexpected sync status: %#v", status)
	}
	for _, path := range []string{source, target} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{"1001 0 `pvf_only`", "2675336 2 `pvf_gold`", "2681762 2 `native_point`"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", path, want, text)
			}
		}
		if strings.Contains(text, "`native_gold`") {
			t.Fatalf("%s kept native duplicate instead of PVF row: %q", path, text)
		}
	}
}

func TestItemInfoFileHelpersHandleLargeRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iteminfo.dat")
	data := []byte("2675336 2 `" + strings.Repeat("x", 128*1024) + "`\n2681762 2 `point`\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	rows := []ceraRow{
		{ItemID: 2675336, Enabled: true, RestockQty: 1},
		{ItemID: 2681762, Enabled: true, RestockQty: 1},
	}
	if err := validateConfiguredCeraItemInfoFile(path, rows); err != nil {
		t.Fatal(err)
	}
	equal, err := itemInfoFileEquals(path, data)
	if err != nil || !equal {
		t.Fatalf("equal=%t err=%v", equal, err)
	}
	changed := append([]byte(nil), data...)
	changed[len(changed)-2] = 'X'
	equal, err = itemInfoFileEquals(path, changed)
	if err != nil || equal {
		t.Fatalf("changed equal=%t err=%v", equal, err)
	}
}

func TestLoadItemInfoRowsSkipsMissingAndReportsReadFailure(t *testing.T) {
	rows, err := loadItemInfoRows([]string{filepath.Join(t.TempDir(), "missing.dat")})
	if err != nil || len(rows) != 0 {
		t.Fatalf("missing target rows=%v err=%v", rows, err)
	}
	if _, err := loadItemInfoRows([]string{"bad\x00path"}); err == nil {
		t.Fatal("invalid target read failure was ignored")
	}
}
