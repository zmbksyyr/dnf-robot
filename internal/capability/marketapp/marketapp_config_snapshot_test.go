package marketapp

import (
	"sync"
	"testing"
)

func TestConfigReturnsDeepCopy(t *testing.T) {
	app := testApp(t)
	app.setConfig(app.cfg)

	got := app.Config()
	got.ItemInfoTargets[0] = "changed"
	got.Restock.StackSizes[0] = 1
	got.Restock.Comments["changed"] = "true"
	*got.Restock.QualityFilter = false
	got.Cera.Items[0].Enabled = false
	got.Auto.Markets[0] = "changed"

	current := app.Config()
	if current.ItemInfoTargets[0] == "changed" || current.Restock.StackSizes[0] == 1 || current.Restock.Comments["changed"] != "" {
		t.Fatalf("Config returned mutable internal slices or maps: %+v", current)
	}
	if current.Restock.QualityFilter == nil || !*current.Restock.QualityFilter || !current.Cera.Items[0].Enabled || current.Auto.Markets[0] == "changed" {
		t.Fatalf("Config returned mutable internal pointers or rows: %+v", current)
	}
}

func TestConfigSnapshotsAndRandomSourceAreConcurrentSafe(t *testing.T) {
	app := testApp(t)
	app.setConfig(app.cfg)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			cfg := app.Config()
			cfg.Restock.EquipmentQtyMin = i%4 + 1
			cfg.Restock.EquipmentQtyMax = cfg.Restock.EquipmentQtyMin + 2
			cfg.Auto.Markets = []string{marketNameAuction, marketNameCera}
			app.setConfig(cfg)
		}
	}()
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = app.Config()
				_ = app.qualityFilterEnabled()
				_ = app.randomRange(1, 10)
				_ = app.randomFloat64()
			}
		}()
	}
	wg.Wait()
}
