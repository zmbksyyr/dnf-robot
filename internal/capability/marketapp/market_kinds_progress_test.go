package marketapp

import (
	"path/filepath"
	"testing"
)

func TestAuctionKindsProgressReportsLiveActualAndOptionalExpected(t *testing.T) {
	app := testApp(t)
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {1001: 3, 9999: 1},
	}}
	itemInfo := filepath.Join(app.configDir, "iteminfo.dat")
	app.cfg.ItemInfoTargets = []string{itemInfo}
	mustWriteText(t, itemInfo, "1001 0 1 1 `a` `a` 1\n2001 2 1 1 `b` `b` 1\n3001 0 1 1 `c` `c` 1\n")
	mustWriteJSON(t, appPaths(app).PVFStackable(), []pvfItem{
		{ID: 1001, Rarity: 0, StackLimit: 1000},
		{ID: 3001, Rarity: 9, StackLimit: 1000},
	})
	mustWriteJSON(t, appPaths(app).PVFEquipment(), []pvfItem{
		{ID: 2001, ItemType: 2, Slot: "title", Rarity: 0},
	})

	progress, err := app.AuctionKindsProgress(true)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Actual != 2 {
		t.Fatalf("actual kinds=%d, want 2", progress.Actual)
	}
	if progress.Expected == nil || *progress.Expected != 2 {
		t.Fatalf("expected kinds=%v, want 2", progress.Expected)
	}

	progress, err = app.AuctionKindsProgress(false)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Actual != 2 || progress.Expected != nil {
		t.Fatalf("poll progress=%+v, want actual only", progress)
	}
}
