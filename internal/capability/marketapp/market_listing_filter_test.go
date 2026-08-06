package marketapp

import "testing"

func TestMarketListingTradePoliciesAreIndependent(t *testing.T) {
	blocked := false
	cfg := DefaultConfig()
	cfg.Restock.EquipmentAllowedRarities = "5"
	cfg.Restock.OtherAllowedRarities = "5"
	equipment := catalogItem{ItemID: 1, Kind: "equipment", Slot: "weapon", Rarity: 5, NoTrade: true, CanAuction: &blocked}
	material := catalogItem{ItemID: 2, Kind: "stackable", Rarity: 5, NoTrade: true, CanAuction: &blocked}

	if !marketListingAllowedWithConfig(equipment, cfg) {
		t.Fatal("default permissive equipment policy filtered a high-rarity item only because of PVF trade fields")
	}
	cfg.Restock.OtherTradePolicy = tradePolicyStrict
	if marketListingAllowedWithConfig(material, cfg) {
		t.Fatal("default strict material policy allowed an explicitly blocked material")
	}

	cfg.Restock.OtherTradePolicy = tradePolicyPermissive
	if !marketListingAllowedWithConfig(material, cfg) {
		t.Fatal("permissive material policy filtered an item only because of PVF trade fields")
	}
	cfg.Restock.EquipmentTradePolicy = tradePolicyStrict
	if marketListingAllowedWithConfig(equipment, cfg) {
		t.Fatal("strict equipment policy allowed an explicitly blocked item")
	}
}

func TestMarketListingAlwaysIncludesEquipmentAndMaterials(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Restock.EquipmentAllowedRarities = "0"
	cfg.Restock.OtherAllowedRarities = "0"
	for _, item := range []catalogItem{
		{ItemID: 1, Kind: "equipment", Slot: "weapon"},
		{ItemID: 2, Kind: "stackable", Attach: "free"},
	} {
		if !marketListingAllowedWithConfig(item, cfg) {
			t.Fatalf("default listing unexpectedly filtered kind %q", item.Kind)
		}
	}
}

func TestListingFilterRemainsEnabledForStrictTradePolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Restock.EquipmentAllowedRarities = "0123456789"
	cfg.Restock.OtherAllowedRarities = "0123456789"
	cfg.Restock.EquipmentTradePolicy = tradePolicyPermissive
	cfg.Restock.OtherTradePolicy = tradePolicyStrict
	if !qualityFilterEnabled(cfg) {
		t.Fatal("strict material policy must keep catalog filtering enabled when all rarities are allowed")
	}
	cfg.Restock.OtherTradePolicy = tradePolicyPermissive
	if qualityFilterEnabled(cfg) {
		t.Fatal("fully permissive policy with all rarities should not require catalog filtering")
	}
}

func TestStrictTradePolicyBlocksAccountAttach(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Restock.EquipmentTradePolicy = tradePolicyStrict
	cfg.Restock.OtherTradePolicy = tradePolicyStrict
	for _, item := range []catalogItem{
		{ItemID: 1, Kind: "equipment", Attach: "account"},
		{ItemID: 2, Kind: "stackable", Attach: " ACCOUNT "},
	} {
		if marketListingAllowedWithConfig(item, cfg) {
			t.Fatalf("strict policy allowed account-bound item: %#v", item)
		}
	}
	cfg.Restock.EquipmentTradePolicy = tradePolicyPermissive
	cfg.Restock.OtherTradePolicy = tradePolicyPermissive
	if !marketListingAllowedWithConfig(catalogItem{ItemID: 1, Kind: "equipment", Attach: "account"}, cfg) {
		t.Fatal("permissive policy filtered account-bound equipment")
	}
}

func TestStrictTradePolicyMatchesNativeAttachRules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Restock.EquipmentTradePolicy = tradePolicyStrict
	cfg.Restock.OtherTradePolicy = tradePolicyStrict
	cases := []struct {
		name string
		item catalogItem
		want bool
	}{
		{name: "free equipment", item: catalogItem{ItemID: 1, Kind: "equipment", Attach: "free", Slot: "weapon"}, want: true},
		{name: "sealed equipment", item: catalogItem{ItemID: 2, Kind: "equipment", Attach: "sealing", Slot: "weapon"}, want: true},
		{name: "trade equipment", item: catalogItem{ItemID: 3, Kind: "equipment", Attach: "trade", Slot: "weapon"}, want: false},
		{name: "trade-delete material", item: catalogItem{ItemID: 4, Kind: "stackable", Attach: "trade delete"}, want: false},
		{name: "trade-limit material", item: catalogItem{ItemID: 5, Kind: "stackable", Attach: "trade limit"}, want: false},
		{name: "free material", item: catalogItem{ItemID: 6, Kind: "stackable", Attach: "free"}, want: true},
		{name: "sealed special equipment", item: catalogItem{ItemID: 7, Kind: "equipment", Attach: "sealing", ItemType: 2, Slot: "title"}, want: false},
	}
	for _, tt := range cases {
		if got := marketListingAllowedWithConfig(tt.item, cfg); got != tt.want {
			t.Fatalf("%s allowed=%v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEquipmentAndOtherRarityFiltersAreIndependent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Restock.EquipmentAllowedRarities = "2"
	cfg.Restock.OtherAllowedRarities = "3"
	if !marketListingAllowedWithConfig(catalogItem{ItemID: 1, Kind: "equipment", Rarity: 2}, cfg) {
		t.Fatal("equipment rarity filter rejected configured rarity")
	}
	if marketListingAllowedWithConfig(catalogItem{ItemID: 2, Kind: "equipment", Rarity: 3}, cfg) {
		t.Fatal("equipment rarity filter used other-item digits")
	}
	if !marketListingAllowedWithConfig(catalogItem{ItemID: 3, Kind: "stackable", Attach: "free", Rarity: 3}, cfg) {
		t.Fatal("other-item rarity filter rejected configured rarity")
	}
}
