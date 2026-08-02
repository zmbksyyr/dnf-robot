package marketapp

import (
	"path/filepath"
	"testing"
)

func TestUpdateConfigKeepsIndependentActionLimits(t *testing.T) {
	app := testApp(t)
	app.configPath = filepath.Join(app.configDir, "market_config.ini")
	app.cfg.Auto.MaxActions = 101
	app.cfg.Restock.MaxActions = 202
	app.cfg.Collector.MaxActions = 303
	app.cfg.Auto.MaxConcurrent = 4
	app.cfg.Restock.MaxConcurrent = 5
	app.cfg.Collector.MaxConcurrent = 6

	quality := false
	if _, err := app.UpdateConfig(ConfigUpdateRequest{QualityFilter: &quality}); err != nil {
		t.Fatal(err)
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
}

func TestUpdateConfigLegacyLimitsStillApplyToEveryMarketJob(t *testing.T) {
	app := testApp(t)
	app.configPath = filepath.Join(app.configDir, "market_config.ini")
	actions, concurrent := 44, 3

	if _, err := app.UpdateConfig(ConfigUpdateRequest{
		MaxActions:    &actions,
		MaxConcurrent: &concurrent,
	}); err != nil {
		t.Fatal(err)
	}
	if app.cfg.Auto.MaxActions != actions || app.cfg.Restock.MaxActions != actions || app.cfg.Collector.MaxActions != actions {
		t.Fatalf("legacy max_actions was not applied to every job: auto=%d restock=%d collect=%d", app.cfg.Auto.MaxActions, app.cfg.Restock.MaxActions, app.cfg.Collector.MaxActions)
	}
	if app.cfg.Auto.MaxConcurrent != concurrent || app.cfg.Restock.MaxConcurrent != concurrent || app.cfg.Collector.MaxConcurrent != concurrent {
		t.Fatalf("legacy max_concurrent was not applied to every job: auto=%d restock=%d collect=%d", app.cfg.Auto.MaxConcurrent, app.cfg.Restock.MaxConcurrent, app.cfg.Collector.MaxConcurrent)
	}
}
