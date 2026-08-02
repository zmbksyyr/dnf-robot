package marketapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"robot/internal/foundation/filewatch"
)

func TestRuntimeFileWatcherReloadsMarketSnapshotsAndRejectsInvalidEdits(t *testing.T) {
	app := testApp(t)
	paths := appPaths(app)
	app.configPath = paths.MarketConfig()
	cfg := app.cfg
	cfg.Auto.Enabled = false
	cfg.Collector.Enabled = true
	cfg.Restock.CustomPriceEnabled = true
	if err := writeMarketConfig(paths.MarketConfig(), cfg); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(t, paths.MarketPrices(), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 3037, MinPrice: 80, MaxPrice: 120, Enabled: true}}})

	poller := filewatch.New(time.Hour, app.RuntimeFileEntries(), nil)
	poller.CheckNow()
	if !app.Config().Collector.Enabled {
		t.Fatal("initial market config was not applied")
	}
	if price, ok := app.customPriceRange(3037); !ok || price.MinPrice != 80 || price.MaxPrice != 120 {
		t.Fatalf("initial price range=%+v ok=%t", price, ok)
	}

	cfg.Collector.Enabled = false
	cfg.Auto.ContinueOnError = false
	if err := writeMarketConfig(paths.MarketConfig(), cfg); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(t, paths.MarketPrices(), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 3037, MinPrice: 200, MaxPrice: 300, Enabled: true}}})
	poller.CheckNow()
	if current := app.Config(); current.Collector.Enabled || current.Auto.ContinueOnError {
		t.Fatalf("updated market config=%+v", current)
	}
	if price, ok := app.customPriceRange(3037); !ok || price.MinPrice != 200 || price.MaxPrice != 300 {
		t.Fatalf("updated price range=%+v ok=%t", price, ok)
	}

	if err := os.WriteFile(paths.MarketConfig(), []byte("[auction_price\nequip_inflate_min = broken\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(t, paths.MarketPrices(), customPriceRangeFile{Version: 1, Items: []customPriceRange{{ItemID: 3037, MinPrice: 500, MaxPrice: 100, Enabled: true}}})
	poller.CheckNow()
	if current := app.Config(); current.Collector.Enabled || current.Auto.ContinueOnError {
		t.Fatalf("invalid edit replaced market config=%+v", current)
	}
	if price, ok := app.customPriceRange(3037); !ok || price.MinPrice != 200 || price.MaxPrice != 300 {
		t.Fatalf("invalid edit replaced price range=%+v ok=%t", price, ok)
	}
}

func TestReloadMarketConfigRefreshesDatabaseAndItemInfoState(t *testing.T) {
	app := testApp(t)
	paths := appPaths(app)
	app.configPath = paths.MarketConfig()
	repository := &clearStockRepository{ensureTables: []string{"new_auction", "new_cera"}}
	app.repository = repository

	current := cloneConfig(app.cfg)
	current.Cera.Items = []ceraRow{{ItemID: 9001, Label: "gold", RestockPrice: 1, RestockQty: 1, Enabled: true}}
	app.setConfig(current)
	app.stateMu.Lock()
	app.dbInitOK = true
	app.dbInit = []string{"old"}
	app.itemInfo = ItemInfoSyncStatus{Error: "old iteminfo error"}
	app.stateMu.Unlock()

	target := filepath.Join(t.TempDir(), "point", "iteminfo.dat")
	mustWriteText(t, target, "9001 2 `gold`\n")
	next := cloneConfig(current)
	next.GameDB = "new_game"
	next.AuctionDB = "new_auction"
	next.CeraDB = "new_cera"
	next.ItemInfoTargets = []string{target}
	if err := writeMarketConfig(paths.MarketConfig(), next); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadMarketConfigFile(paths.MarketConfig()); err != nil {
		t.Fatal(err)
	}

	if repository.ensureCalls != 1 {
		t.Fatalf("database config change ensured tables %d times, want 1", repository.ensureCalls)
	}
	app.stateMu.RLock()
	dbReady := app.dbInitOK
	itemInfo := cloneItemInfoStatus(app.itemInfo)
	app.stateMu.RUnlock()
	if !dbReady {
		t.Fatal("database derived state was not refreshed")
	}
	if itemInfo.Error != "" || len(itemInfo.Targets) != 1 || itemInfo.Targets[0] != target {
		t.Fatalf("iteminfo derived state was not refreshed: %+v", itemInfo)
	}

	ceraOnly := cloneConfig(next)
	ceraOnly.Cera.Items = []ceraRow{{ItemID: 9002, Label: "missing", RestockPrice: 1, RestockQty: 1, Enabled: true}}
	if err := writeMarketConfig(paths.MarketConfig(), ceraOnly); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadMarketConfigFile(paths.MarketConfig()); err != nil {
		t.Fatal(err)
	}
	if repository.ensureCalls != 1 {
		t.Fatalf("cera-only change reinitialized databases: calls=%d", repository.ensureCalls)
	}
	app.stateMu.RLock()
	itemInfo = cloneItemInfoStatus(app.itemInfo)
	app.stateMu.RUnlock()
	if !strings.Contains(itemInfo.Error, "9002") {
		t.Fatalf("cera-only change did not refresh iteminfo validation: %+v", itemInfo)
	}
}

func TestReloadMarketConfigRestartsStoppedEnabledAutoWithoutDuplicateLoop(t *testing.T) {
	app := testApp(t)
	paths := appPaths(app)
	app.configPath = paths.MarketConfig()
	cfg := cloneConfig(app.cfg)
	cfg.Auto.Enabled = true
	cfg.Auto.InitialDelayMS = 60000
	app.setConfig(cfg)
	if err := writeMarketConfig(paths.MarketConfig(), cfg); err != nil {
		t.Fatal(err)
	}

	if err := app.reloadMarketConfigFile(paths.MarketConfig()); err != nil {
		t.Fatal(err)
	}
	if !app.AutoRunning() {
		t.Fatal("enabled auto loop was not recovered")
	}
	app.autoMu.Lock()
	firstDone := app.autoDone
	app.autoMu.Unlock()

	if err := app.reloadMarketConfigFile(paths.MarketConfig()); err != nil {
		t.Fatal(err)
	}
	app.autoMu.Lock()
	secondDone := app.autoDone
	app.autoMu.Unlock()
	if secondDone != firstDone {
		t.Fatal("identical reload replaced the running auto loop")
	}
}

func TestMarketAutoScheduleChangeDetection(t *testing.T) {
	base := AutoCfg{Enabled: true, Markets: []string{"auction", "cera"}, InitialDelayMS: 1000, IntervalMS: 60000, ContinueOnError: true}
	if marketAutoScheduleChanged(base, base) {
		t.Fatal("identical auto config requires a restart")
	}
	policyOnly := base
	policyOnly.ContinueOnError = false
	if marketAutoScheduleChanged(base, policyOnly) {
		t.Fatal("non-scheduling auto policy requires a restart")
	}
	interval := base
	interval.IntervalMS++
	if !marketAutoScheduleChanged(base, interval) {
		t.Fatal("interval change did not require an auto-loop restart")
	}
}
