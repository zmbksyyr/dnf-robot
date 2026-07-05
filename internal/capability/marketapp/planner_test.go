package marketapp

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestClearSystemMarketStockDeletesDBRowsAndResetsQueues(t *testing.T) {
	repo := &clearStockRepository{counts: map[string]int{
		DefaultConfig().AuctionDB: 3,
		DefaultConfig().CeraDB:    2,
	}}
	app := testApp()
	app.repository = repo
	app.configDir = t.TempDir()
	app.auctionQueue = []uint32{1001}
	app.auctionRejected = []uint32{1002}
	app.auctionRejectedTick = 3
	app.auctionQueueSource = "pvf"

	result, err := app.ClearSystemMarketStock()
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 5 {
		t.Fatalf("deleted = %d, want 5", result.Deleted)
	}
	if len(app.auctionQueue) != 0 || len(app.auctionRejected) != 0 || app.auctionRejectedTick != 0 || app.auctionQueueSource != "" {
		t.Fatalf("queues not reset: queue=%v rejected=%v tick=%d source=%q", app.auctionQueue, app.auctionRejected, app.auctionRejectedTick, app.auctionQueueSource)
	}
	if repo.collectCalls != 0 {
		t.Fatalf("system stock clear used collect path, calls=%d", repo.collectCalls)
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
	dir := t.TempDir()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	mustWriteText(t, app.cfg.ItemInfoTargets[0], "10075 0 1 1 1 1 1 1 1 1 1 1 1 40 `a` `a` 11001\r\n30075 0 1 1 1 1 1 1 1 1 1 1 1 70 `b` `b` 11001\r\n")
	app.cfg.Restock.EquipmentQtyMin = 2
	app.cfg.Restock.EquipmentQtyMax = 2
	catalog := map[uint32]catalogItem{
		10075: {ItemID: 10075, Kind: "equipment", Level: 40, Attach: "trade", Slot: "coat", Price: 100},
		30075: {ItemID: 30075, Kind: "equipment", Level: 70, Attach: "trade", Slot: "coat", Price: 100},
	}

	rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{30075: 1}, 2)
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
	if len(rows) != 1 || rows[0].ItemID != 30075 {
		t.Fatalf("queue did not rotate stocked item to the back: %#v", rows)
	}
}

func TestAuctionQueueUsesCurrentItemInfoIntersection(t *testing.T) {
	app := testApp()
	dir := t.TempDir()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	mustWriteText(t, app.cfg.ItemInfoTargets[0], "10075 0 1 1 1 1 1 1 1 1 1 1 1 40 `known` `known` 11001\r\n")
	catalog := map[uint32]catalogItem{
		10075:     {ItemID: 10075, Kind: "equipment", Level: 40, Attach: "trade", Slot: "coat", Price: 100},
		100050020: {ItemID: 100050020, Kind: "equipment", Level: 80, Attach: "trade", Slot: "coat", Price: 100},
	}

	rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ItemID != 10075 {
		t.Fatalf("selected rows = %#v, want only current iteminfo intersection", rows)
	}
}

func TestAuctionRejectedQueueUsesLowWeightCooldownBudget(t *testing.T) {
	app := testApp()
	app.cfg.Restock.EquipmentQtyMin = 1
	app.cfg.Restock.EquipmentQtyMax = 1
	catalog := map[uint32]catalogItem{}
	normalIDs := []uint32{}
	for i := uint32(1); i <= 120; i++ {
		id := 10000 + i
		normalIDs = append(normalIDs, id)
		catalog[id] = catalogItem{ItemID: id, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100}
	}
	rejectedIDs := []uint32{20001, 20002, 20003}
	for _, id := range rejectedIDs {
		catalog[id] = catalogItem{ItemID: id, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100}
	}
	app.auctionQueue = append([]uint32(nil), normalIDs...)
	app.auctionRejected = append([]uint32(nil), rejectedIDs...)
	app.auctionQueueSource = "pvf"

	for round := 1; round < auctionRejectedRetryEvery; round++ {
		rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{}, 100)
		if err != nil {
			t.Fatal(err)
		}
		rejectedSet := idSet(rejectedIDs)
		for _, row := range rows {
			if rejectedSet[row.ItemID] {
				t.Fatalf("round %d selected rejected row before cooldown: %#v", round, rows)
			}
		}
	}

	rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 100 {
		t.Fatalf("rows = %d, want 100", len(rows))
	}
	normal, rejected := 0, 0
	rejectedSet := idSet(rejectedIDs)
	for _, row := range rows {
		if rejectedSet[row.ItemID] {
			rejected++
		} else {
			normal++
		}
	}
	if normal != 99 || rejected != 1 {
		t.Fatalf("budget split normal=%d rejected=%d, want 99/1 rows=%#v", normal, rejected, rows)
	}
}

func TestAuctionRejectedQueueReturnsStockedItemsToNormal(t *testing.T) {
	app := testApp()
	app.cfg.Restock.EquipmentQtyMin = 1
	app.cfg.Restock.EquipmentQtyMax = 1
	catalog := map[uint32]catalogItem{
		10075: {ItemID: 10075, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100},
		30075: {ItemID: 30075, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100},
	}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionQueueSource = "pvf"

	rows, err := app.nextAuctionQueueRows(true, catalog, map[uint32]int{30075: 1}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ItemID != 10075 {
		t.Fatalf("selected rows = %#v, want only normal missing item", rows)
	}
	if len(app.auctionRejected) != 0 {
		t.Fatalf("rejected queue = %#v, want empty", app.auctionRejected)
	}
	if !queueContains(app.auctionQueue, 30075) {
		t.Fatalf("stocked rejected item did not return to normal queue: %#v", app.auctionQueue)
	}
}

func TestMarkAuctionExplicitRejectedMovesID(t *testing.T) {
	app := testApp()
	app.auctionQueue = []uint32{10075, 30075, 10075}
	app.markAuctionExplicitRejected(10075)

	if queueContains(app.auctionQueue, 10075) {
		t.Fatalf("normal queue still contains rejected id: %#v", app.auctionQueue)
	}
	if len(app.auctionRejected) != 1 || app.auctionRejected[0] != 10075 {
		t.Fatalf("rejected queue = %#v, want [10075]", app.auctionRejected)
	}
}

func TestMarketPolicyRebuildsQueueAfterRepeatedZeroKinds(t *testing.T) {
	app := testApp()
	app.configDir = t.TempDir()
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {},
	}}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionRejectedTick = 7
	app.auctionQueueSource = "pvf_iteminfo"

	first := app.marketAutoPolicy("auction", app.cfg.Auto)
	if first.MaxActions != app.cfg.Auto.MaxActions || first.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("first policy changed pressure: %#v", first)
	}
	if len(app.auctionQueue) == 0 || len(app.auctionRejected) == 0 {
		t.Fatalf("first zero round should only observe queues: normal=%v rejected=%v", app.auctionQueue, app.auctionRejected)
	}

	second := app.marketAutoPolicy("auction", app.cfg.Auto)
	if second.MaxActions != app.cfg.Auto.MaxActions || second.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("second policy should rebuild only: %#v", second)
	}
	if len(app.auctionQueue) != 0 || len(app.auctionRejected) != 0 || app.auctionRejectedTick != 0 || app.auctionQueueSource != "" {
		t.Fatalf("second zero round did not reset queues: normal=%v rejected=%v tick=%d source=%q", app.auctionQueue, app.auctionRejected, app.auctionRejectedTick, app.auctionQueueSource)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != 2000 || third.MaxConcurrent != 4 {
		t.Fatalf("third zero round pressure = %#v, want 2000/4", third)
	}
	status := app.policy["auction"]
	if status.Mode != marketPolicyModeDegraded || status.ZeroKindRounds != 3 {
		t.Fatalf("policy status = %#v, want degraded round 3", status)
	}
}

func TestMarketPolicyTracksZeroAuctionCandidates(t *testing.T) {
	dir := t.TempDir()
	app := testApp()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {10075: 1},
	}}
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 10075, "price": 100, "attach": "trade", "slot": "weapon"},
	})
	mustWriteText(t, filepath.Join(dir, "iteminfo.dat"), "99999 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n")
	app.auctionQueue = []uint32{10075}
	app.auctionQueueSource = "pvf_iteminfo"

	first := app.marketAutoPolicy("auction", app.cfg.Auto)
	if first.MaxActions != app.cfg.Auto.MaxActions || first.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("first zero candidate policy changed pressure: %#v", first)
	}

	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	if len(app.auctionQueue) != 0 || app.auctionQueueSource != "" {
		t.Fatalf("second zero candidate round did not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != 2000 || third.MaxConcurrent != 4 {
		t.Fatalf("third zero candidate pressure = %#v, want 2000/4", third)
	}
	status := app.policy["auction"]
	if status.ZeroCandidateRounds != 3 || status.Candidates != 0 || status.CandidateSource == "" {
		t.Fatalf("candidate policy status = %#v", status)
	}
}

func TestMarketPolicyDetectsStagnantAuctionGrowth(t *testing.T) {
	dir := t.TempDir()
	app := testApp()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	stock := map[uint32]int{10075: 1}
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: stock,
	}}
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 10075, "price": 100, "attach": "trade", "slot": "weapon"},
		{"id": 10076, "price": 100, "attach": "trade", "slot": "weapon"},
		{"id": 10077, "price": 100, "attach": "trade", "slot": "weapon"},
	})
	mustWriteText(t, filepath.Join(dir, "iteminfo.dat"), ""+
		"10075 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n"+
		"10076 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n"+
		"10077 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n")
	app.auctionQueue = []uint32{10076}
	app.auctionQueueSource = "pvf_iteminfo"

	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	second := app.marketAutoPolicy("auction", app.cfg.Auto)
	if second.MaxActions != app.cfg.Auto.MaxActions || second.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("second stagnant round should observe only: %#v", second)
	}
	if len(app.auctionQueue) == 0 || app.auctionQueueSource == "" {
		t.Fatalf("first comparable stagnant round should not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != app.cfg.Auto.MaxActions || third.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("third stagnant round should rebuild only: %#v", third)
	}
	if len(app.auctionQueue) != 0 || app.auctionQueueSource != "" {
		t.Fatalf("stagnant recovery did not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	fourth := app.marketAutoPolicy("auction", app.cfg.Auto)
	if fourth.MaxActions != 2000 || fourth.MaxConcurrent != 4 {
		t.Fatalf("fourth stagnant pressure = %#v, want 2000/4", fourth)
	}
	status := app.policy["auction"]
	if status.StagnantRounds != 3 || status.Candidates != 3 || status.DBKinds != 1 {
		t.Fatalf("stagnant status = %#v", status)
	}

	stock[10076] = 1
	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	status = app.policy["auction"]
	if status.StagnantRounds != 0 || status.KindDelta <= 0 {
		t.Fatalf("growth did not reset stagnant status: %#v", status)
	}
}

func TestMarketPolicyBlockedStateRecordsReason(t *testing.T) {
	app := testApp()
	app.configDir = t.TempDir()
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionQueueSource = "pvf_iteminfo"

	app.markMarketPolicyBlocked("auction", "df_game_r is not running")
	status := app.policy["auction"]
	if status.Mode != marketPolicyModeDegraded || status.Reason != "df_game_r is not running" {
		t.Fatalf("blocked status = %#v", status)
	}
	if status.QueueNormal != 1 || status.QueueRejected != 1 || status.QueueSource != "pvf_iteminfo" {
		t.Fatalf("blocked queue snapshot = %#v", status)
	}
}

func TestRecordMarketPolicyJobKeepsPolicyAndAddsFeedback(t *testing.T) {
	app := testApp()
	app.configDir = t.TempDir()
	app.policy = map[string]MarketPolicyStatus{
		"auction": {
			Market:              "auction",
			Mode:                marketPolicyModeRecover,
			Reason:              "recovering",
			DBKinds:             10,
			EffectiveMaxActions: 100,
		},
	}
	app.recordMarketPolicyJob("auction", JobSummary{
		Status: "partial_failed",
		Error:  "1 actions failed",
		Plan:   &PlanSummary{Actions: 3},
		Actions: []ActionEntry{
			{OK: true},
			{OK: false},
			{OK: true, Error: "write timeout"},
		},
	})

	status := app.policy["auction"]
	if status.Mode != marketPolicyModeRecover || status.Reason != "recovering" || status.DBKinds != 10 {
		t.Fatalf("policy judgement was overwritten: %#v", status)
	}
	if status.LastJobStatus != "partial_failed" || status.LastJobError != "1 actions failed" || status.LastPlanActions != 3 || status.LastActionResults != 3 || status.LastActionFailed != 2 {
		t.Fatalf("job feedback not recorded: %#v", status)
	}
}

func TestNextMarketPolicyStateIsPureCounterLogic(t *testing.T) {
	prev := MarketPolicyStatus{
		DBKinds:             10,
		StagnantRounds:      1,
		ZeroCandidateRounds: 1,
		UpdatedAt:           time.Now(),
	}
	state := nextMarketPolicyState("auction", 10, marketCandidateSnapshot{Count: 20}, prev)
	if state.mode != marketPolicyModeRecover || state.stagnantRounds != 2 || state.kindDelta != 0 {
		t.Fatalf("stagnant state = %#v", state)
	}

	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.stagnantRounds != 0 || state.kindDelta != 1 || state.mode != marketPolicyModeNormal {
		t.Fatalf("growth state = %#v", state)
	}

	state = nextMarketPolicyState("auction", 10, marketCandidateSnapshot{Count: 0}, prev)
	if state.zeroCandidateRounds != 2 || state.mode != marketPolicyModeRecover {
		t.Fatalf("zero candidate state = %#v", state)
	}

	prev = MarketPolicyStatus{DBKinds: 10, LastActionResults: 20, LastActionFailed: 10, UpdatedAt: time.Now()}
	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.actionFailureRounds != 1 || state.mode != marketPolicyModeRecover {
		t.Fatalf("high failure state = %#v", state)
	}

	prev = MarketPolicyStatus{DBKinds: 10, LastActionResults: 19, LastActionFailed: 19, UpdatedAt: time.Now()}
	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.actionFailureRounds != 0 {
		t.Fatalf("low sample failure state = %#v", state)
	}
}

func TestMarketPolicyDegradesAfterRepeatedActionFailures(t *testing.T) {
	app := testApp()
	status := MarketPolicyStatus{Market: "auction", ActionFailureRounds: 2}
	policy := app.applyAuctionPolicyActions(marketCandidateSnapshot{Count: 100}, &status, marketAutoPolicy{MaxActions: 10000, MaxConcurrent: 8})

	if status.Mode != marketPolicyModeDegraded || policy.MaxActions != 2000 || policy.MaxConcurrent != 4 {
		t.Fatalf("repeated failure policy status=%#v policy=%#v", status, policy)
	}
}

func TestMarketPolicyHealthCompletion(t *testing.T) {
	status := MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeNormal, DBKinds: 10, Candidates: 20, CandidateSource: "iteminfo.dat"}
	status.applyHealth()
	if status.Health != "healthy" || status.Completion != 100 {
		t.Fatalf("healthy status = %#v", status)
	}

	status = MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeRecover, DBKinds: 0, Candidates: 20, CandidateSource: "iteminfo.dat"}
	status.applyHealth()
	if status.Health != "blocked" || status.Completion != 40 {
		t.Fatalf("blocked status = %#v", status)
	}

	status = MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeNormal, DBKinds: 10, Candidates: 20, CandidateSource: "iteminfo.dat", LastActionResults: 20, LastActionFailed: 10}
	status.applyHealth()
	if status.Health != "warning" || status.Completion != 70 {
		t.Fatalf("warning status = %#v", status)
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

func TestPlanAuctionHandlesNonCreatureSpecialTypesWithUniqueAddInfo(t *testing.T) {
	app := testApp()
	app.repository = &clearStockRepository{maxAddInfo: specialAddInfoBase + 7}
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		2001: {ItemID: 2001, Kind: "equipment", ItemType: 2, Slot: "title name", Attach: "trade", Price: 100},
	}
	app.planAuction([]restockRow{{ItemID: 2001, Quantity: 1, Enabled: true}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 || len(result.Skipped) != 0 {
		t.Fatalf("special plan actions=%#v skipped=%#v", result.Actions, result.Skipped)
	}
	action := result.Actions[0]
	if action.Kind != "title" || action.CountAddInfo != specialAddInfoBase+8 || action.Count != 1 {
		t.Fatalf("unexpected special action: %#v", action)
	}
}

func TestPlanAuctionCreatesCreatureItemInstanceForCreatureSpecial(t *testing.T) {
	app := testApp()
	repo := &clearStockRepository{creatureIDs: []int32{4567}}
	app.repository = repo
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		3001: {ItemID: 3001, Kind: "equipment", ItemType: 30, Slot: "creature", Attach: "trade", Price: 100},
	}
	app.planAuction([]restockRow{{ItemID: 3001, Quantity: 1, Enabled: true}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 || len(result.Skipped) != 0 {
		t.Fatalf("creature plan actions=%#v skipped=%#v", result.Actions, result.Skipped)
	}
	action := result.Actions[0]
	if action.Kind != "creature" || action.CountAddInfo != 4567 || action.OwnerID == 0 {
		t.Fatalf("unexpected creature action: %#v", action)
	}
	if len(repo.creatureCreates) != 1 {
		t.Fatalf("creature creates = %#v", repo.creatureCreates)
	}
	create := repo.creatureCreates[0]
	if create.dbName != app.cfg.GameDB || create.ownerID != action.OwnerID || create.itemID != action.ItemID {
		t.Fatalf("unexpected creature create: %#v action=%#v", create, action)
	}
}

func TestPlanAuctionSkipsCreatureWhenInstanceCreationFails(t *testing.T) {
	app := testApp()
	app.repository = &clearStockRepository{createCreatureErr: errors.New("insert failed")}
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		3001: {ItemID: 3001, Kind: "equipment", ItemType: 30, Slot: "creature", Attach: "trade", Price: 100},
	}
	app.planAuction([]restockRow{{ItemID: 3001, Quantity: 1, Enabled: true}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 0 || len(result.Skipped) != 1 {
		t.Fatalf("creature plan actions=%#v skipped=%#v", result.Actions, result.Skipped)
	}
	if result.Skipped[0].Reason != "creature_instance_failed" {
		t.Fatalf("creature skip reason = %q", result.Skipped[0].Reason)
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

func mustWriteText(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
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

func TestSpecialAuctionKindClassifiesReferenceTypes(t *testing.T) {
	cases := []struct {
		name string
		item catalogItem
		want string
	}{
		{name: "title", item: catalogItem{Kind: "equipment", ItemType: 2, Slot: "title name"}, want: "title"},
		{name: "creature", item: catalogItem{Kind: "equipment", ItemType: 30, Slot: "creature"}, want: "creature"},
		{name: "avatar", item: catalogItem{Kind: "equipment", ItemType: 23, Slot: "coatavatar"}, want: "avatar"},
		{name: "artifact red", item: catalogItem{Kind: "equipment", Slot: "artifact red"}, want: "artifact red"},
		{name: "normal weapon", item: catalogItem{Kind: "equipment", ItemType: 1, Slot: "weapon"}, want: ""},
	}
	for _, tt := range cases {
		if got := specialAuctionKind(tt.item); got != tt.want {
			t.Fatalf("%s special kind = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCatalogAuctionCandidateCountsSeparatesSpecialTypes(t *testing.T) {
	catalog := map[uint32]catalogItem{
		1001: {ItemID: 1001, Kind: "equipment", ItemType: 1, Slot: "weapon", Attach: "trade"},
		2001: {ItemID: 2001, Kind: "equipment", ItemType: 2, Slot: "title name", Attach: "trade"},
		3001: {ItemID: 3001, Kind: "equipment", ItemType: 30, Slot: "creature", Attach: "trade"},
		4001: {ItemID: 4001, Kind: "equipment", Slot: "artifact red", Attach: "trade"},
		5001: {ItemID: 5001, Kind: "blocked", ItemType: 1, Slot: "weapon"},
	}
	normal, special := catalogAuctionCandidateCounts(catalog, map[uint32]bool{1001: true, 2001: true, 3001: true, 4001: true, 5001: true})
	if normal != 1 || special != 3 {
		t.Fatalf("candidate counts normal=%d special=%d, want 1/3", normal, special)
	}
}

func TestRiskyPVFItemAllowsHighLevelEquipmentWhenItemInfoCapsLevel(t *testing.T) {
	if isRiskyPVFItem(catalogItem{Kind: "equipment", Level: 70, Attach: "sealing", Slot: "weapon"}) {
		t.Fatal("level 70 equipment should be allowed")
	}
	if isRiskyPVFItem(catalogItem{Kind: "equipment", Level: 85, Attach: "sealing", Slot: "weapon"}) {
		t.Fatal("level 85 equipment should be allowed after iteminfo level capping")
	}
	if isRiskyPVFItem(catalogItem{Kind: "stackable", Level: 99, Slot: "material"}) {
		t.Fatal("stackable level should not use equipment level filter")
	}
}

type clearStockRepository struct {
	counts            map[string]int
	stock             map[string]map[uint32]int
	maxAddInfo        int32
	collectCalls      int
	creatureIDs       []int32
	createCreatureErr error
	creatureCreates   []creatureCreateCall
}

type creatureCreateCall struct {
	dbName  string
	ownerID uint32
	itemID  uint32
}

func (r *clearStockRepository) EnsureMarketTables([]string, time.Time) ([]string, error) {
	return nil, nil
}

func (r *clearStockRepository) LoadCollectRows(string, string, uint32, bool) ([]collectRow, error) {
	return nil, nil
}

func (r *clearStockRepository) LoadSystemCollectRows(string, string, uint32) ([]collectRow, error) {
	r.collectCalls++
	return nil, nil
}

func (r *clearStockRepository) LoadMarketStock(dbName string, _ uint32, _ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	for id, count := range r.stock[dbName] {
		out[id] = count
	}
	return out, nil
}

func (r *clearStockRepository) LoadMaxAddInfo(string, int32) (int32, error) {
	return r.maxAddInfo, nil
}

func (r *clearStockRepository) CreateCreatureItem(dbName string, ownerID uint32, itemID uint32) (int32, error) {
	if r.createCreatureErr != nil {
		return 0, r.createCreatureErr
	}
	r.creatureCreates = append(r.creatureCreates, creatureCreateCall{dbName: dbName, ownerID: ownerID, itemID: itemID})
	if len(r.creatureIDs) == 0 {
		return int32(7000 + len(r.creatureCreates)), nil
	}
	id := r.creatureIDs[0]
	r.creatureIDs = r.creatureIDs[1:]
	return id, nil
}

func (r *clearStockRepository) CountSystemStock(dbName string, _ uint32) (int, error) {
	return r.counts[dbName], nil
}

func (r *clearStockRepository) DeleteSystemStock(dbName string, _ uint32) (int64, error) {
	count := r.counts[dbName]
	r.counts[dbName] = 0
	return int64(count), nil
}

var _ Repository = (*clearStockRepository)(nil)
