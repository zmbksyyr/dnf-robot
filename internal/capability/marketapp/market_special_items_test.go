package marketapp

import (
	"errors"
	"testing"
)

func TestPlanAuctionHandlesNonCreatureSpecialTypesWithUniqueAddInfo(t *testing.T) {
	app := testApp(t)
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

func TestPlanAuctionKeepsPetArtifactItemType(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		63500: {ItemID: 63500, Kind: "equipment", ItemType: 31, Slot: "artifact red", Attach: "trade", Price: 100},
	}
	app.planAuction([]restockRow{{ItemID: 63500, Quantity: 1, Enabled: true}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 || len(result.Skipped) != 0 {
		t.Fatalf("artifact plan actions=%#v skipped=%#v", result.Actions, result.Skipped)
	}
	action := result.Actions[0]
	if action.Kind != "artifact red" || action.ItemType != 31 {
		t.Fatalf("unexpected artifact action: %#v", action)
	}
}

func TestPlanAuctionCreatesCreatureItemInstanceForCreatureSpecial(t *testing.T) {
	app := testApp(t)
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
	app := testApp(t)
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
		{name: "avatar", item: catalogItem{Kind: "equipment", ItemType: 23, Slot: "coatavatar"}, want: ""},
		{name: "artifact red", item: catalogItem{Kind: "equipment", Slot: "artifact red"}, want: "artifact red"},
		{name: "normal weapon", item: catalogItem{Kind: "equipment", ItemType: 1, Slot: "weapon"}, want: ""},
	}
	for _, tt := range cases {
		if got := specialAuctionKind(tt.item); got != tt.want {
			t.Fatalf("%s special kind = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestMarketCandidateKeepsSpecialTypes(t *testing.T) {
	cases := []catalogItem{
		{ItemID: 2001, Kind: "equipment", ItemType: 2, Slot: "title name"},
		{ItemID: 3001, Kind: "equipment", ItemType: 30, Slot: "creature"},
		{ItemID: 4001, Kind: "equipment", Slot: "artifact red"},
	}
	for _, item := range cases {
		if !marketCandidate(item) {
			t.Fatalf("special item filtered from market candidates: %#v", item)
		}
	}
}

func TestMarketCandidateFiltersAvatar(t *testing.T) {
	cases := []catalogItem{
		{ItemID: 5001, Kind: "equipment", ItemType: 20, Slot: "hatavatar"},
		{ItemID: 5002, Kind: "equipment", ItemType: 1, Slot: "coatavatar"},
	}
	for _, item := range cases {
		if marketCandidate(item) {
			t.Fatalf("avatar item kept in market candidates: %#v", item)
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
		6001: {ItemID: 6001, Kind: "equipment", ItemType: 23, Slot: "coatavatar", Attach: "trade"},
	}
	normal, special := catalogAuctionCandidateCounts(catalog, map[uint32]bool{1001: true, 2001: true, 3001: true, 4001: true, 5001: true, 6001: true})
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

func TestSummarizePlanCountsAuctionabilitySkips(t *testing.T) {
	summary := summarizePlan(nil, []SkippedItem{
		{Reason: "not_auctionable"},
		{Reason: "avatar_not_auctionable"},
		{Reason: "requires_add_info"},
		{Reason: "risky_special_type"},
	}, 0)
	if summary.NotAuctionable != 3 || summary.Risky != 1 || summary.Skipped != 4 {
		t.Fatalf("summary = %#v", summary)
	}
}
