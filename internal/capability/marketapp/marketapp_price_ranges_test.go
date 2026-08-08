package marketapp

import (
	"testing"
)

func TestCustomPriceRangeOverridesFullEquipmentFormula(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.CustomPriceEnabled = true
	mustWriteJSON(t, appPaths(app).MarketPrices(), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 31056, MinPrice: 700000, MaxPrice: 700000, Enabled: true}}})
	app.refreshCustomPriceRanges()

	price := app.auctionUnitPriceFor(catalogItem{ItemID: 31056, Kind: "equipment"}, 1000, 8, 13)
	if price != 700000 {
		t.Fatalf("custom price=%d want 700000", price)
	}
	low, high := app.auctionPriceBounds(catalogItem{ItemID: 31056, Kind: "equipment", Price: 1000})
	if low != 700000 || high != 700000 {
		t.Fatalf("custom bounds=%d..%d", low, high)
	}
}

func TestEquipmentFormulaBoundsIncludeMultiplierUpgradeAndRandomRate(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.RandLow = 0.9
	app.cfg.Restock.RandHigh = 1.1
	app.cfg.Restock.EquipInflateMin = 5
	app.cfg.Restock.EquipInflateMax = 8
	app.cfg.Restock.UpgradeMin = 7
	app.cfg.Restock.UpgradeMax = 13
	app.cfg.Restock.UpgradePriceRate = 0.08

	low, high := app.auctionPriceBounds(catalogItem{ItemID: 31056, Kind: "equipment", Price: 1000})
	if low != 7020 || high != 17952 {
		t.Fatalf("formula bounds=%d..%d want 7020..17952", low, high)
	}
}

func TestAuctionQualityRatesApplyToEquipmentAndStackableItems(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.LevelPriceRate = 0.15
	app.cfg.Restock.RarityPriceRate = 0.30
	app.cfg.Restock.UpgradePriceRate = 0
	app.cfg.Restock.RandLow = 1
	app.cfg.Restock.RandHigh = 1

	equipment := catalogItem{Kind: "equipment", Level: 85, Rarity: 4}
	if got := app.auctionUnitPriceFor(equipment, 1000, 1, 0); got != 7810 {
		t.Fatalf("equipment price=%d want 7810", got)
	}
	stackable := catalogItem{Kind: "stackable", Level: 12, Rarity: 2}
	if got := app.auctionUnitPriceFor(stackable, 1000, 99, 31); got != 2080 {
		t.Fatalf("stackable price=%d want 2080", got)
	}
}

func TestAuctionQualityRatesAreIncludedInCollectorBounds(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.LevelPriceRate = 0.15
	app.cfg.Restock.RarityPriceRate = 0.30
	app.cfg.Restock.EquipInflateMin = 1
	app.cfg.Restock.EquipInflateMax = 2
	app.cfg.Restock.UpgradeMin = 0
	app.cfg.Restock.UpgradeMax = 0
	app.cfg.Restock.RandLow = 1
	app.cfg.Restock.RandHigh = 1

	low, high := app.auctionPriceBounds(catalogItem{Kind: "equipment", Level: 5, Rarity: 2, Price: 1000})
	if low != 1839 || high != 3679 {
		t.Fatalf("quality bounds=%d..%d want 1839..3679", low, high)
	}
}

func TestCollectPricePolicyUsesHighAndLowProbabilities(t *testing.T) {
	app := testApp(t)
	app.cfg.Collector.PriceRangeEnabled = true
	app.cfg.Collector.InRangeProbability = 1
	app.cfg.Collector.OutRangeProbability = 0
	app.cfg.Restock.CustomPriceEnabled = true
	mustWriteJSON(t, appPaths(app).MarketPrices(), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 3037, MinPrice: 80, MaxPrice: 120, Enabled: true}}})
	app.repository = &clearStockRepository{collectRows: map[string][]collectRow{
		app.cfg.AuctionDB: {
			{Market: marketNameAuction, AuctionID: 1, ItemID: 3037, Count: 10, StartPrice: -1, InstantPrice: 1000},
			{Market: marketNameAuction, AuctionID: 2, ItemID: 3037, Count: 10, StartPrice: -1, InstantPrice: 3000},
		},
	}}

	result, err := app.CollectPlan(CollectRequest{Market: marketNameAuction})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].AuctionID != 1 {
		t.Fatalf("selected actions=%#v want only in-range auction 1", result.Actions)
	}
}

func TestInvalidCustomPriceFileFallsBackToFormula(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.CustomPriceEnabled = true
	mustWriteText(t, appPaths(app).MarketPrices(), "{broken")
	app.refreshCustomPriceRanges()

	if app.priceRangeStatus.Error == "" {
		t.Fatal("invalid custom price file did not report an error")
	}
	price := app.auctionUnitPriceFor(catalogItem{ItemID: 3037, Kind: "stackable"}, 100, 1, 0)
	if price != 100 {
		t.Fatalf("formula fallback price=%d want 100", price)
	}
}
