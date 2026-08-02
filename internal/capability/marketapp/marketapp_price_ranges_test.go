package marketapp

import (
	"path/filepath"
	"testing"
)

func TestCustomPriceRangeOverridesFullEquipmentFormula(t *testing.T) {
	app := testApp(t)
	app.cfg.Restock.CustomPriceEnabled = true
	app.cfg.Restock.CustomPriceFile = "prices.json"
	mustWriteJSON(t, filepath.Join(app.configDir, "prices.json"), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 31056, MinPrice: 700000, MaxPrice: 700000, Enabled: true}}})
	app.refreshCustomPriceRanges()

	price := app.auctionUnitPriceFor(31056, 1000, true, 8, 13)
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

func TestCollectPricePolicyUsesHighAndLowProbabilities(t *testing.T) {
	app := testApp(t)
	app.cfg.Collector.PriceRangeEnabled = true
	app.cfg.Collector.InRangeProbability = 1
	app.cfg.Collector.OutRangeProbability = 0
	app.cfg.Restock.CustomPriceEnabled = true
	app.cfg.Restock.CustomPriceFile = "prices.json"
	mustWriteJSON(t, filepath.Join(app.configDir, "prices.json"), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 3037, MinPrice: 80, MaxPrice: 120, Enabled: true}}})
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
	app.cfg.Restock.CustomPriceFile = "prices.json"
	mustWriteText(t, filepath.Join(app.configDir, "prices.json"), "{broken")
	app.refreshCustomPriceRanges()

	if app.priceRangeStatus.Error == "" {
		t.Fatal("invalid custom price file did not report an error")
	}
	price := app.auctionUnitPriceFor(3037, 100, false, 1, 0)
	if price != 100 {
		t.Fatalf("formula fallback price=%d want 100", price)
	}
}
