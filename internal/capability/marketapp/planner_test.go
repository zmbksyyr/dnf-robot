package marketapp

import (
	"encoding/json"
	"math/rand"
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
		4000:  {ItemID: 4000, Kind: "stackable", Price: 7, StackLimit: 1000},
		31056: {ItemID: 31056, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100},
		31057: {ItemID: 31057, Kind: "blocked", Price: 100},
	}
	rows := app.catalogAuctionRows(catalog)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].ItemID != 4000 || rows[0].Quantity != 500 || rows[0].StackSize != 500 {
		t.Fatalf("unexpected stackable row: %#v", rows[0])
	}
	if rows[1].ItemID != 31056 || rows[1].Quantity < 2 || rows[1].Quantity > 5 || rows[1].StackSize != 1 {
		t.Fatalf("unexpected equipment row: %#v", rows[1])
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
