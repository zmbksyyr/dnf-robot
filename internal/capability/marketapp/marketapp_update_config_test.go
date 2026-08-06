package marketapp

import "testing"

func TestUpdateConfigKeepsIndependentActionLimits(t *testing.T) {
	app := testApp(t)
	app.configPath = appPaths(app).MarketConfig()
	app.cfg.Auto.MaxActions = 101
	app.cfg.Restock.MaxActions = 202
	app.cfg.Collector.MaxActions = 303
	app.cfg.Auto.MaxConcurrent = 4
	app.cfg.Restock.MaxConcurrent = 5
	app.cfg.Collector.MaxConcurrent = 6

	allowed := "43004"
	if _, err := app.UpdateConfig(ConfigUpdateRequest{EquipmentAllowedRarities: &allowed}); err != nil {
		t.Fatal(err)
	}
	if app.cfg.Restock.EquipmentAllowedRarities != "034" {
		t.Fatalf("allowed rarities=%q, want normalized 034", app.cfg.Restock.EquipmentAllowedRarities)
	}
	if app.cfg.Auto.MaxActions != 101 || app.cfg.Restock.MaxActions != 202 || app.cfg.Collector.MaxActions != 303 {
		t.Fatalf("unrelated update changed action limits: auto=%d restock=%d collect=%d", app.cfg.Auto.MaxActions, app.cfg.Restock.MaxActions, app.cfg.Collector.MaxActions)
	}

	autoActions, restockActions, collectActions := 111, 222, 333
	autoConcurrent, restockConcurrent, collectConcurrent := 7, 8, 9
	qtyMin, qtyMax, delay := 3, 7, 25
	if _, err := app.UpdateConfig(ConfigUpdateRequest{
		AutoMaxActions:         &autoActions,
		RestockMaxActions:      &restockActions,
		CollectorMaxActions:    &collectActions,
		AutoMaxConcurrent:      &autoConcurrent,
		RestockMaxConcurrent:   &restockConcurrent,
		CollectorMaxConcurrent: &collectConcurrent,
		EquipmentQtyMin:        &qtyMin,
		EquipmentQtyMax:        &qtyMax,
		StackSizes:             []int{100, 500},
		BlockedItemIDs:         []uint32{300, 100, 300, 0},
		RestockPerItemDelayMS:  &delay,
	}); err != nil {
		t.Fatal(err)
	}
	if app.cfg.Auto.MaxActions != 111 || app.cfg.Restock.MaxActions != 222 || app.cfg.Collector.MaxActions != 333 {
		t.Fatalf("separate action limits not applied: %+v %+v %+v", app.cfg.Auto, app.cfg.Restock, app.cfg.Collector)
	}
	if app.cfg.Auto.MaxConcurrent != 7 || app.cfg.Restock.MaxConcurrent != 8 || app.cfg.Collector.MaxConcurrent != 9 {
		t.Fatalf("separate concurrency limits not applied: %+v %+v %+v", app.cfg.Auto, app.cfg.Restock, app.cfg.Collector)
	}
	if app.cfg.Restock.EquipmentQtyMin != 3 || app.cfg.Restock.EquipmentQtyMax != 7 || app.cfg.Restock.PerItemDelayMS != 25 || len(app.cfg.Restock.StackSizes) != 2 {
		t.Fatalf("restock web settings not applied: %+v", app.cfg.Restock)
	}
	if got := app.cfg.Restock.BlockedItemIDs; len(got) != 2 || got[0] != 100 || got[1] != 300 {
		t.Fatalf("blocked item IDs = %v, want [100 300]", got)
	}
}

func TestApplyListingConfigLockedDoesNotChangeRuntimeParameters(t *testing.T) {
	app := testApp(t)
	app.configPath = appPaths(app).MarketConfig()
	app.cfg.Auto.IntervalMS = 98765
	app.cfg.Restock.MaxConcurrent = 17
	allowed, equipmentPolicy, materialPolicy := "056", tradePolicyStrict, tradePolicyPermissive
	qty := 4
	cfg, err := app.applyListingConfigLocked(ConfigUpdateRequest{
		EquipmentAllowedRarities: &allowed, EquipmentTradePolicy: &equipmentPolicy, OtherTradePolicy: &materialPolicy,
		EquipmentQtyMin: &qty, EquipmentQtyMax: &qty, BlockedItemIDs: []uint32{20, 10, 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restock.EquipmentAllowedRarities != "056" || cfg.Restock.EquipmentTradePolicy != tradePolicyStrict || cfg.Restock.OtherTradePolicy != tradePolicyPermissive {
		t.Fatalf("listing settings not applied: %+v", cfg.Restock)
	}
	if got := cfg.Restock.BlockedItemIDs; len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("listing blocked item IDs = %v, want [10 20]", got)
	}
	if cfg.Auto.IntervalMS != 98765 || cfg.Restock.MaxConcurrent != 17 {
		t.Fatalf("runtime parameters changed: auto=%+v restock=%+v", cfg.Auto, cfg.Restock)
	}
}
