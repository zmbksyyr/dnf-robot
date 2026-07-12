package marketapp

import (
	"path/filepath"
	"testing"
)

func TestCatalogAuctionRowsUsePVFOnly(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.StackSizes = []int{500}
	catalog := map[uint32]catalogItem{
		4000:      {ItemID: 4000, Kind: "stackable", Price: 7, StackLimit: 1000},
		31056:     {ItemID: 31056, Kind: "equipment", Level: 40, Attach: "trade", Slot: "weapon", Price: 100},
		100050020: {ItemID: 100050020, Kind: "equipment", Level: 80, Attach: "trade", Slot: "coat", Price: 100},
		31057:     {ItemID: 31057, Kind: "blocked", Price: 100},
	}
	rows := app.catalogAuctionRows(catalog)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].ItemID != 100050020 || rows[1].ItemID != 31056 {
		t.Fatalf("equipment rows are not level-desc first: %#v", rows)
	}
	if rows[2].ItemID != 4000 || rows[2].Quantity != 500 || rows[2].StackSize != 500 {
		t.Fatalf("unexpected stackable row: %#v", rows[0])
	}
	if rows[1].Quantity < 2 || rows[1].Quantity > 5 || rows[1].StackSize != 1 {
		t.Fatalf("unexpected equipment row: %#v", rows[1])
	}
}

func TestLoadCatalogUsesPVFJSONOnly(t *testing.T) {
	dir := t.TempDir()
	app := testApp(t)
	app.configDir = dir
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{
		{"id": 4000, "price": 7, "stack_limit": 1000},
	})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 31056, "price": 100, "attach": "trade", "slot": "weapon"},
	})

	catalog, err := app.loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog[4000]; !ok {
		t.Fatal("stackable PVF id missing")
	}
	if _, ok := catalog[31056]; !ok {
		t.Fatal("equipment PVF id missing")
	}
}

func TestFallbackAuctionRowsKeepEmbeddedBasePrices(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.StackSizes = []int{500}
	rows, err := app.fallbackAuctionRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("fallback rows are empty")
	}
	if rows[0].ItemID == 0 || rows[0].SystemPrice <= 0 || rows[0].StackSize != 500 || rows[0].Source != "fallback_seed" {
		t.Fatalf("unexpected fallback row: %#v", rows[0])
	}
}
