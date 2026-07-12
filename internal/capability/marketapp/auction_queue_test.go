package marketapp

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuctionQueueSkipsStockedItemsAndRotates(t *testing.T) {
	app := testApp(t)
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

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{30075: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
	if len(rows) != 1 || rows[0].ItemID != 10075 {
		t.Fatalf("selected rows = %#v, want only missing low item", rows)
	}

	selection, err = app.nextAuctionQueueSelection(true, catalog, map[uint32]int{10075: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	rows = selection.Rows
	if len(rows) != 1 || rows[0].ItemID != 30075 {
		t.Fatalf("queue did not rotate stocked item to the back: %#v", rows)
	}
}

func TestAuctionQueueUsesCurrentItemInfoIntersection(t *testing.T) {
	app := testApp(t)
	dir := t.TempDir()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	mustWriteText(t, app.cfg.ItemInfoTargets[0], "10075 0 1 1 1 1 1 1 1 1 1 1 1 40 `known` `known` 11001\r\n")
	catalog := map[uint32]catalogItem{
		10075:     {ItemID: 10075, Kind: "equipment", Level: 40, Attach: "trade", Slot: "coat", Price: 100},
		100050020: {ItemID: 100050020, Kind: "equipment", Level: 80, Attach: "trade", Slot: "coat", Price: 100},
	}

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
	if len(rows) != 1 || rows[0].ItemID != 10075 {
		t.Fatalf("selected rows = %#v, want only current iteminfo intersection", rows)
	}
}

func TestAuctionQueueSkipsNullNameItemInfoRows(t *testing.T) {
	app := testApp(t)
	dir := t.TempDir()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	mustWriteText(t, app.cfg.ItemInfoTargets[0], ""+
		"2600557 2 1 1 1 1 1 1 1 1 1 1 1 80 `name2_2600557 == NULL, stackable/Stackable.kor.str : ` 33001\r\n"+
		"2600558 2 1 1 1 1 1 1 1 1 1 1 1 80 `valid` `valid` 33001\r\n")
	catalog := map[uint32]catalogItem{
		2600557: {ItemID: 2600557, Kind: "stackable", StackLimit: 1000, Price: 100},
		2600558: {ItemID: 2600558, Kind: "stackable", StackLimit: 1000, Price: 100},
	}

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
	if len(rows) != 1 || rows[0].ItemID != 2600558 {
		t.Fatalf("selected rows = %#v, want only non-null-name iteminfo row", rows)
	}
}

func TestReloadAuctionQueuesOnlyUpgradesToPVFItemInfo(t *testing.T) {
	app := testApp(t)
	app.auctionQueue = []uint32{10075}
	app.auctionQueueSource = marketQueueSourceFallback

	if err := app.reloadAuctionQueues(false, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(app.auctionQueue, []uint32{10075}) || app.auctionQueueSource != marketQueueSourceFallback {
		t.Fatalf("fallback reload should not replace existing queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	dir := t.TempDir()
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	mustWriteText(t, app.cfg.ItemInfoTargets[0], "30075 0 1 1 1 1 1 1 1 1 1 1 1 70 `b` `b` 11001\r\n")
	catalog := map[uint32]catalogItem{
		10075: {ItemID: 10075, Kind: "equipment", Level: 40, Attach: "trade", Slot: "coat", Price: 100},
		30075: {ItemID: 30075, Kind: "equipment", Level: 70, Attach: "trade", Slot: "coat", Price: 100},
	}

	if err := app.reloadAuctionQueues(true, catalog); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(app.auctionQueue, []uint32{30075}) || app.auctionQueueSource != marketQueueSourcePVFItemInfo {
		t.Fatalf("pvf iteminfo reload did not replace fallback queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}
}

func TestAuctionRejectedQueueUsesLowWeightCooldownBudget(t *testing.T) {
	app := testApp(t)
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
	app.auctionQueueSource = "pvf_iteminfo"

	for round := 1; round < auctionRejectedRetryEvery; round++ {
		selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 100)
		if err != nil {
			t.Fatal(err)
		}
		rows := selection.Rows
		rejectedSet := idSet(rejectedIDs)
		for _, row := range rows {
			if rejectedSet[row.ItemID] {
				t.Fatalf("round %d selected rejected row before cooldown: %#v", round, rows)
			}
		}
	}

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
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

func TestAuctionQueueSelectionReportsBudgets(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.EquipmentQtyMin = 1
	app.cfg.Restock.EquipmentQtyMax = 1
	catalog := map[uint32]catalogItem{}
	for i := uint32(1); i <= 120; i++ {
		id := 10000 + i
		catalog[id] = catalogItem{ItemID: id, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100}
		app.auctionQueue = append(app.auctionQueue, id)
	}
	app.auctionSpecialQueue = []uint32{20001, 20002}
	catalog[20001] = catalogItem{ItemID: 20001, Kind: "equipment", ItemType: 2, Slot: "title name", Attach: "trade", Price: 100}
	catalog[20002] = catalogItem{ItemID: 20002, Kind: "equipment", Slot: "artifact red", Attach: "trade", Price: 100}
	app.auctionRejected = []uint32{30001}
	app.auctionRejectedTick = auctionRejectedRetryEvery - 1
	catalog[30001] = catalogItem{ItemID: 30001, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100}
	app.auctionQueueSource = "pvf_iteminfo"

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Budget.Normal != 90 || selection.Budget.Special != 9 || selection.Budget.Rejected != 1 {
		t.Fatalf("budget = %#v, want normal=90 special=9 rejected=1", selection.Budget)
	}
	if selection.Selected.Normal != 90 || selection.Selected.Special != 2 || selection.Selected.Rejected != 1 {
		t.Fatalf("selected = %#v, want normal=90 special=2 rejected=1", selection.Selected)
	}
	if len(selection.Rows) != 93 {
		t.Fatalf("rows = %d, want 93 because special queue has only two candidates", len(selection.Rows))
	}
}

func TestAuctionSpecialQueueGetsDedicatedBudget(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.EquipmentQtyMin = 1
	app.cfg.Restock.EquipmentQtyMax = 1
	catalog := map[uint32]catalogItem{}
	for i := uint32(1); i <= 50; i++ {
		id := 10000 + i
		catalog[id] = catalogItem{ItemID: id, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100}
	}
	catalog[20001] = catalogItem{ItemID: 20001, Kind: "equipment", ItemType: 2, Slot: "title name", Attach: "trade", Price: 100}
	catalog[20002] = catalogItem{ItemID: 20002, Kind: "equipment", Slot: "artifact red", Attach: "trade", Price: 100}
	for id := range catalog {
		if specialAuctionKind(catalog[id]) != "" {
			app.auctionSpecialQueue = append(app.auctionSpecialQueue, id)
		} else {
			app.auctionQueue = append(app.auctionQueue, id)
		}
	}
	app.auctionQueueSource = "pvf_iteminfo"

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{}, 20)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
	special := 0
	for _, row := range rows {
		if specialAuctionKind(row.marketItem()) != "" {
			special++
		}
	}
	if special == 0 {
		t.Fatalf("special queue received no budget: rows=%#v normal_queue=%d special_queue=%d", rows, len(app.auctionQueue), len(app.auctionSpecialQueue))
	}
}

func TestAuctionSpecialQueuePrioritizesPetArtifacts(t *testing.T) {
	catalog := map[uint32]catalogItem{
		415510139: {ItemID: 415510139, Kind: "equipment", ItemType: 20, Slot: "avatar", Attach: "trade", Price: 100},
		20001:     {ItemID: 20001, Kind: "equipment", ItemType: 2, Slot: "title name", Attach: "trade", Price: 100},
		30001:     {ItemID: 30001, Kind: "equipment", ItemType: 30, Slot: "creature", Attach: "trade", Price: 100},
		63500:     {ItemID: 63500, Kind: "equipment", ItemType: 31, Slot: "artifact red", Attach: "trade", Price: 100},
		64000:     {ItemID: 64000, Kind: "equipment", ItemType: 32, Slot: "artifact blue", Attach: "trade", Price: 100},
	}
	_, special := catalogAuctionIDsByType(catalog, nil)

	want := []uint32{63500, 64000, 30001, 20001}
	if !reflect.DeepEqual(special, want) {
		t.Fatalf("special order = %v, want %v", special, want)
	}
}

func TestAuctionRowIDsDropsEmptyRows(t *testing.T) {
	ids := auctionRowIDs([]restockRow{{ItemID: 0}, {ItemID: 10075}, {ItemID: 0}, {ItemID: 30075}})
	want := []uint32{10075, 30075}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestAuctionRejectedQueueReturnsStockedItemsToNormal(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.EquipmentQtyMin = 1
	app.cfg.Restock.EquipmentQtyMax = 1
	catalog := map[uint32]catalogItem{
		10075: {ItemID: 10075, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100},
		30075: {ItemID: 30075, Kind: "equipment", Attach: "trade", Slot: "weapon", Price: 100},
	}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionQueueSource = "pvf_iteminfo"

	selection, err := app.nextAuctionQueueSelection(true, catalog, map[uint32]int{30075: 1}, 10)
	if err != nil {
		t.Fatal(err)
	}
	rows := selection.Rows
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
	app := testApp(t)
	app.auctionQueue = []uint32{10075, 30075, 10075}
	app.auctionSpecialQueue = []uint32{20075, 10075}
	app.markAuctionExplicitRejected(10075)

	if queueContains(app.auctionQueue, 10075) {
		t.Fatalf("normal queue still contains rejected id: %#v", app.auctionQueue)
	}
	if queueContains(app.auctionSpecialQueue, 10075) {
		t.Fatalf("special queue still contains rejected id: %#v", app.auctionSpecialQueue)
	}
	if len(app.auctionRejected) != 1 || app.auctionRejected[0] != 10075 {
		t.Fatalf("rejected queue = %#v, want [10075]", app.auctionRejected)
	}
	state := app.auctionRejectedMeta[10075]
	if state.Reason != "explicit_rejected" || state.Count != 1 || state.First.IsZero() || state.Last.IsZero() {
		t.Fatalf("rejected meta = %#v", state)
	}
}

func TestApplyAuctionActionFeedbackOnlyRejectsFailedRegisters(t *testing.T) {
	app := testApp(t)
	app.auctionQueue = []uint32{10075, 10076}

	app.applyAuctionActionFeedback(ActionEntry{
		Action:    Action{Market: marketNameAuction, ItemID: 10075},
		OK:        true,
		AuctionID: 123,
	}, nil)
	if queueContains(app.auctionRejected, 10075) {
		t.Fatalf("successful auction action was rejected: %#v", app.auctionRejected)
	}

	app.applyAuctionActionFeedback(ActionEntry{
		Action: Action{Market: marketNameAuction, ItemID: 10076},
		OK:     false,
	}, nil)
	if !queueContains(app.auctionRejected, 10076) {
		t.Fatalf("failed auction action was not rejected: %#v", app.auctionRejected)
	}
	if state := app.auctionRejectedMeta[10076]; state.Reason != "missing_auction_id" || state.Count != 1 {
		t.Fatalf("rejected meta = %#v", state)
	}
}

func TestMarketDecisionLogsAuctionRejectedDetails(t *testing.T) {
	app := testApp(t)
	app.auctionRejected = []uint32{10075, 20075}
	app.auctionRejectedTick = 3
	app.auctionRejectedMeta = map[uint32]auctionRejectedState{
		10075: {Reason: "missing_auction_id", Count: 2, First: time.Now(), Last: time.Now()},
		20075: {Reason: "executor_error", Count: 1, First: time.Now(), Last: time.Now()},
	}

	decision := marketDecisionSnapshot{
		AuctionBudget:   auctionQueueBudget{Normal: 90, Special: 9, Rejected: 1},
		AuctionSelected: auctionQueueCounts{Normal: 90, Special: 2, Rejected: 1},
	}
	decision.captureQueues(app)
	text := decision.String()
	for _, want := range []string{"queue_rejected=2", "rejected_tracked=2", "rejected_retry_in=7", "rejected_reasons=missing_auction_id:2,executor_error:1", "budget_normal=90", "budget_special=9", "budget_rejected=1", "selected_normal=90", "selected_special=2", "selected_rejected=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("decision log missing %q in %s", want, text)
		}
	}
}

func TestQueueSnapshotFeedsDecisionAndPolicy(t *testing.T) {
	snapshot := auctionQueueSnapshot{
		Normal:          3,
		Special:         2,
		Rejected:        1,
		RejectedTracked: 1,
		RejectedRetryIn: 6,
		RejectedReasons: "executor_error:1",
		Source:          marketQueueSourcePVFItemInfo,
	}
	decision := marketDecisionSnapshot{}
	decision.applyQueueSnapshot(snapshot)
	policy := MarketPolicyStatus{}
	policy.applyQueueSnapshot(snapshot)

	if decision.QueueNormal != policy.QueueNormal || decision.QueueSpecial != policy.QueueSpecial || decision.QueueRejected != policy.QueueRejected || decision.QueueSource != policy.QueueSource {
		t.Fatalf("queue snapshot mismatch: decision=%#v policy=%#v", decision, policy)
	}
	if decision.RejectedTracked != policy.QueueRejectedTracked || decision.RejectedRetryIn != policy.QueueRejectedRetryIn || decision.RejectedReasons != snapshot.RejectedReasons {
		t.Fatalf("rejected snapshot mismatch: decision=%#v policy=%#v", decision, policy)
	}
}
