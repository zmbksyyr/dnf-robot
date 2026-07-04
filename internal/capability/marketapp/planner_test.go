package marketapp

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp() *App {
	cfg := DefaultConfig()
	cfg.Restock.RandLow = 1
	cfg.Restock.RandHigh = 1
	return &App{cfg: cfg, rand: rand.New(rand.NewSource(1))}
}

func TestDefaultConfigDoesNotExposeUnknownCycleLimit(t *testing.T) {
	data, err := json.Marshal(DefaultConfig().Restock)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "unknown_per_cycle") {
		t.Fatalf("restock config still exposes unknown_per_cycle: %s", data)
	}
}

func TestPlanAuctionStackableSplitsQuantity(t *testing.T) {
	app := testApp()
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		3037: {ItemID: 3037, Name: "cube", Kind: "stackable", StackLimit: 1000},
	}
	app.planAuction([]restockRow{{
		ItemID: 3037, SystemPrice: 88, Quantity: 2500, StackSize: 1000, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(result.Actions))
	}
	counts := []int32{result.Actions[0].Count, result.Actions[1].Count, result.Actions[2].Count}
	wantCounts := []int32{1000, 1000, 500}
	for i := range wantCounts {
		if counts[i] != wantCounts[i] {
			t.Fatalf("count[%d] = %d, want %d", i, counts[i], wantCounts[i])
		}
	}
	if result.Actions[0].InstantPrice != 88000 || result.Actions[2].InstantPrice != 44000 {
		t.Fatalf("unexpected prices: %#v", result.Actions)
	}
}

func TestPlanAuctionStackableClampsToPVFStackLimit(t *testing.T) {
	app := testApp()
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		36: {ItemID: 36, Name: "speaker", Kind: "stackable", StackLimit: 1},
	}
	app.planAuction([]restockRow{{
		ItemID: 36, SystemPrice: 200000, Quantity: 3, StackSize: 1000, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(result.Actions))
	}
	for _, action := range result.Actions {
		if action.Count != 1 || action.InstantPrice != 200000 {
			t.Fatalf("unexpected clamped action: %#v", action)
		}
	}
}

func TestPlanAuctionEquipmentUsesSingleRecordPrice(t *testing.T) {
	app := testApp()
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		31056: {ItemID: 31056, Name: "weapon", Kind: "equipment", Attach: "trade", Slot: "weapon"},
	}
	app.planAuction([]restockRow{{
		ItemID: 31056, SystemPrice: 88888, Quantity: 2, StackSize: 99, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(result.Actions))
	}
	for _, action := range result.Actions {
		if action.Count != 1 || action.InstantPrice <= 88888 || action.CountAddInfo != 0 {
			t.Fatalf("unexpected equipment action: %#v", action)
		}
		if action.Upgrade < 7 || action.Upgrade > 13 {
			t.Fatalf("equipment upgrade = %d, want 7..13", action.Upgrade)
		}
		if action.ExtraAddInfo < 1 || action.ExtraAddInfo > 7 {
			t.Fatalf("equipment extra_add_info = %d, want 1..7", action.ExtraAddInfo)
		}
	}
}

func TestPlanAuctionKeepsMissingItemBatchTogether(t *testing.T) {
	app := testApp()
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		10075:     {ItemID: 10075, Name: "low", Kind: "equipment", Attach: "trade", Slot: "coat"},
		100050020: {ItemID: 100050020, Name: "high", Kind: "equipment", Attach: "trade", Slot: "coat"},
	}
	app.planAuction([]restockRow{
		{ItemID: 10075, SystemPrice: 1000, Quantity: 3, StackSize: 1, Enabled: true},
		{ItemID: 100050020, SystemPrice: 1000, Quantity: 3, StackSize: 1, Enabled: true},
	}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 6 {
		t.Fatalf("actions = %d, want 6", len(result.Actions))
	}
	for i := 0; i < 3; i++ {
		if result.Actions[i].ItemID != 10075 {
			t.Fatalf("low item batch was split: %#v", result.Actions[:4])
		}
	}
	for i := 3; i < 6; i++ {
		if result.Actions[i].ItemID != 100050020 {
			t.Fatalf("high item batch was split: %#v", result.Actions[2:])
		}
	}
}

func TestAuctionQueueSkipsStockedItemsAndRotates(t *testing.T) {
	app := testApp()
	app.cfg.Restock.EquipmentQtyMin = 2
	app.cfg.Restock.EquipmentQtyMax = 2
	catalog := map[uint32]catalogItem{
		10075:     {ItemID: 10075, Kind: "equipment", Level: 40, Attach: "trade", Slot: "coat", Price: 100},
		100050020: {ItemID: 100050020, Kind: "equipment", Level: 80, Attach: "trade", Slot: "coat", Price: 100},
	}

	rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{100050020: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ItemID != 10075 {
		t.Fatalf("selected rows = %#v, want only missing low item", rows)
	}

	rows, err = app.nextAuctionQueueRows(true, catalog, map[uint32]int{10075: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ItemID != 100050020 {
		t.Fatalf("queue did not rotate stocked item to the back: %#v", rows)
	}
}

func TestAuctionUnitPriceUsesUpgradeAndRefine(t *testing.T) {
	app := testApp()
	low := app.auctionUnitPrice(1000, true, 5, 7, 1)
	highUpgrade := app.auctionUnitPrice(1000, true, 5, 13, 1)
	highRefine := app.auctionUnitPrice(1000, true, 5, 7, 7)

	if highUpgrade <= low {
		t.Fatalf("high upgrade price = %d, want > %d", highUpgrade, low)
	}
	if highRefine <= low {
		t.Fatalf("high refine price = %d, want > %d", highRefine, low)
	}
}

func TestPlanCeraUsesPointBuyNowShape(t *testing.T) {
	app := testApp()
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		2675345: {ItemID: 2675345, Kind: "stackable"},
	}
	app.planCera([]ceraRow{{
		ItemID: 2675345, Label: "1000w", RestockPrice: 1200, RestockQty: 1, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(result.Actions))
	}
	action := result.Actions[0]
	if action.StartPrice != -1 || action.InstantPrice != 1200 || action.CountAddInfo != 1 {
		t.Fatalf("unexpected cera action: %#v", action)
	}
}

func TestCatalogAuctionRowsUsePVFOnly(t *testing.T) {
	app := testApp()
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
	app := testApp()
	app.configDir = dir
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{
		{"id": 4000, "price": 7, "stack_limit": 1000},
	})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 31056, "price": 100, "attach": "trade", "slot": "weapon"},
		{"id": 3100060, "price": 100, "attach": "trade", "slot": "amulet"},
	})
	mustWriteJSON(t, filepath.Join(dir, "pvf_iteminfo_catalog.json"), []map[string]interface{}{
		{"id": 4000, "category": 13002},
		{"id": 31056, "category": 10302},
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
	if _, ok := catalog[3100060]; ok {
		t.Fatal("PVF id missing from iteminfo catalog was promoted")
	}
}

func TestLoadItemInfoJSONCatalogIncludesGoldPackages(t *testing.T) {
	dir := t.TempDir()
	app := testApp()
	app.configDir = dir
	mustWriteJSON(t, filepath.Join(dir, "pvf_iteminfo_catalog.json"), []map[string]interface{}{
		{"id": 2675336, "rarity": 2, "level": 1, "name": "100w_gold", "category": 13002},
	})

	catalog, err := app.loadItemInfoJSONCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog[2675336]; !ok {
		t.Fatal("gold package id missing from iteminfo json catalog")
	}
	if catalog[2675336].Kind != "stackable" {
		t.Fatalf("unexpected iteminfo catalog kind: %#v", catalog[2675336])
	}
}

func mustWriteJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackAuctionRowsKeepEmbeddedBasePrices(t *testing.T) {
	app := testApp()
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

func TestKnownZeroSuccessEquipmentFilter(t *testing.T) {
	cases := []struct {
		name string
		item catalogItem
		want bool
	}{
		{name: "normal equipment", item: catalogItem{Kind: "equipment", Attach: "trade", Slot: "weapon"}, want: false},
		{name: "missing attach equipment", item: catalogItem{Kind: "equipment", Slot: "weapon"}, want: true},
		{name: "free avatar equipment", item: catalogItem{Kind: "equipment", Attach: "free", Slot: "coatavatar"}, want: true},
		{name: "free creature equipment", item: catalogItem{Kind: "equipment", Attach: "free", Slot: "creature"}, want: true},
		{name: "stackable ignores attach", item: catalogItem{Kind: "stackable", Attach: "", Slot: "etc"}, want: false},
	}
	for _, tt := range cases {
		if got := isKnownZeroSuccessEquipment(tt.item); got != tt.want {
			t.Fatalf("%s filter = %v, want %v", tt.name, got, tt.want)
		}
	}
}
