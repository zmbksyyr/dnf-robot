package marketapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestockJobWaitsForAuctionDBFactAfterAck(t *testing.T) {
	app := testApp(t)
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {},
	}}
	job := JobSummary{Status: MarketJobStatusRunning}

	app.applyRestockDBConfirmation(&job, []Action{{
		Market:    marketNameAuction,
		Operation: "register",
		ItemID:    1001,
	}})

	if job.Status != MarketJobStatusPendingDB {
		t.Fatalf("status = %q, want %q", job.Status, MarketJobStatusPendingDB)
	}
	if !strings.Contains(job.Error, "DB fact") {
		t.Fatalf("error = %q, want DB fact confirmation message", job.Error)
	}
}

func TestRestockJobSucceedsWhenAuctionDBFactExists(t *testing.T) {
	app := testApp(t)
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {1001: 1},
	}}
	job := JobSummary{Status: MarketJobStatusRunning}

	app.applyRestockDBConfirmation(&job, []Action{{
		Market:    marketNameAuction,
		Operation: "register",
		ItemID:    1001,
	}})

	if job.Status != MarketJobStatusSuccess {
		t.Fatalf("status = %q, want %q error=%q", job.Status, MarketJobStatusSuccess, job.Error)
	}
	if job.Error != "" {
		t.Fatalf("error = %q, want empty", job.Error)
	}
}

func TestAppendLogWithoutConfigDirDoesNotWriteCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	app := &App{}

	app.appendLog(LogEvent{Type: "test", Status: marketLogStatusSuccess})

	if marketLogPath("") != "" {
		t.Fatalf("empty config dir should not resolve to a relative log path")
	}
	if _, err := os.Stat(filepath.Join(dir, marketLogFile)); !os.IsNotExist(err) {
		t.Fatalf("appendLog wrote runtime log in current directory, stat err=%v", err)
	}
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
	app := testApp(t)
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
	app := testApp(t)
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

func TestPlanAuctionStackableAvoidsInt32TotalOverflow(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		63041: {ItemID: 63041, Name: "fallback stack", Kind: "stackable"},
	}
	app.planAuction([]restockRow{{
		ItemID:      63041,
		SystemPrice: 10276010,
		Quantity:    2000,
		StackSize:   2000,
		Enabled:     true,
		Source:      marketRowSourceFallbackSeed,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(result.Actions))
	}
	action := result.Actions[0]
	if action.Count <= 0 || action.Count >= 2000 {
		t.Fatalf("count = %d, want reduced positive count", action.Count)
	}
	if action.CountAddInfo != action.Count {
		t.Fatalf("count_add_info = %d, want %d", action.CountAddInfo, action.Count)
	}
	if action.TotalPrice <= 0 || action.InstantPrice <= 0 || action.StartPrice < 0 {
		t.Fatalf("unexpected non-positive prices: %#v", action)
	}
	if int64(action.UnitPrice)*int64(action.Count) != int64(action.TotalPrice) {
		t.Fatalf("total price mismatch: unit=%d count=%d total=%d", action.UnitPrice, action.Count, action.TotalPrice)
	}
	if action.TotalPrice > maxInt32 || action.InstantPrice > maxInt32 {
		t.Fatalf("price exceeds int32 max: %#v", action)
	}
}

func TestPlanAuctionEquipmentUsesSingleRecordPrice(t *testing.T) {
	app := testApp(t)
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
		if action.Endurance != defaultAuctionEquipmentEndurance {
			t.Fatalf("equipment endurance = %d, want default", action.Endurance)
		}
		if action.ExtraAddInfo != 0 {
			t.Fatalf("equipment extra_add_info = %d, want 0", action.ExtraAddInfo)
		}
	}
}

func TestPlanAuctionEquipmentKeepsExplicitEndurance(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		31056: {ItemID: 31056, Name: "weapon", Kind: "equipment", Attach: "trade", Slot: "weapon"},
	}
	app.planAuction([]restockRow{{
		ItemID: 31056, SystemPrice: 88888, Quantity: 1, StackSize: 1, Endurance: 345, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Endurance != 345 {
		t.Fatalf("equipment endurance = %d, want explicit value", result.Actions[0].Endurance)
	}
}

func TestPlanAuctionEquipmentProtocolTypeUsesSealFlag(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		10016: {ItemID: 10016, Name: "sealing coat", Kind: "equipment", ItemType: 3, Attach: "sealing", Slot: "coat"},
		10017: {ItemID: 10017, Name: "trade coat", Kind: "equipment", ItemType: 3, Attach: "trade", Slot: "coat"},
	}
	app.planAuction([]restockRow{
		{ItemID: 10016, SystemPrice: 1000, Quantity: 1, StackSize: 1, Enabled: true},
		{ItemID: 10017, SystemPrice: 1000, Quantity: 1, StackSize: 1, Enabled: true},
	}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(result.Actions))
	}
	if result.Actions[0].ItemType != 1 {
		t.Fatalf("sealing protocol item_type = %d, want 1", result.Actions[0].ItemType)
	}
	if result.Actions[1].ItemType != 0 {
		t.Fatalf("trade protocol item_type = %d, want 0", result.Actions[1].ItemType)
	}
}

func TestAuctionPlanHelpersKeepFilteringAndBatchRules(t *testing.T) {
	if reason := auctionPlanSkipReason(restockRow{}, catalogItem{Kind: "blocked"}); reason != "not_auctionable" {
		t.Fatalf("blocked skip reason = %q", reason)
	}
	if reason := auctionPlanSkipReason(restockRow{}, catalogItem{Kind: "equipment", ItemType: 20, Slot: "coatavatar"}); reason != "avatar_not_auctionable" {
		t.Fatalf("avatar skip reason = %q", reason)
	}
	if reason := auctionPlanSkipReason(restockRow{SealFlag: 1}, catalogItem{Kind: "stackable"}); reason != "requires_add_info" {
		t.Fatalf("seal skip reason = %q", reason)
	}
	if stack := auctionPlanStackSize(restockRow{StackSize: 1000}, catalogItem{Kind: "stackable", StackLimit: 200}, false); stack != 200 {
		t.Fatalf("stack size = %d, want 200", stack)
	}
	if stack := auctionPlanStackSize(restockRow{StackSize: 1000}, catalogItem{Kind: "equipment", StackLimit: 200}, true); stack != 1 {
		t.Fatalf("equipment stack size = %d, want 1", stack)
	}
	if source := auctionActionSource(restockRow{}); source != marketActionSourceUnknown {
		t.Fatalf("empty source = %q", source)
	}
}

func TestPlanAuctionKeepsMissingItemBatchTogether(t *testing.T) {
	app := testApp(t)
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

func TestAuctionTargetRecordsUsesFinalStackLimit(t *testing.T) {
	row := restockRow{ItemID: 36, Kind: "stackable", Quantity: 3, StackSize: 1000, StackLimit: 1}
	if records := auctionTargetRecords(row); records != 3 {
		t.Fatalf("records = %d, want 3", records)
	}
}

func TestAuctionUnitPriceUsesUpgradeOnly(t *testing.T) {
	app := testApp(t)
	low := app.auctionUnitPrice(1000, true, 5, 7)
	highUpgrade := app.auctionUnitPrice(1000, true, 5, 13)

	if highUpgrade <= low {
		t.Fatalf("high upgrade price = %d, want > %d", highUpgrade, low)
	}
}

func TestSummarizePlanDoesNotBucketUnknownSkipReason(t *testing.T) {
	summary := summarizePlan(nil, []SkippedItem{{Reason: "unknown_skip_reason"}}, 0)
	if summary.Special != 0 || summary.Skipped != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
