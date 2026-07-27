package marketapp

import (
	"testing"
	"time"
)

func TestClearSystemMarketStockDeletesDBRowsAndResetsQueues(t *testing.T) {
	repo := &clearStockRepository{counts: map[string]int{
		DefaultConfig().AuctionDB: 3,
		DefaultConfig().CeraDB:    2,
	}, creatureCount: 4}
	app := testApp(t)
	app.repository = repo
	app.configDir = t.TempDir()
	app.auctionQueue = []uint32{1001}
	app.auctionSpecialQueue = []uint32{1003}
	app.auctionRejected = []uint32{1002}
	app.auctionRejectedMeta = map[uint32]auctionRejectedState{1002: {Reason: "executor_error", Count: 2, First: time.Now(), Last: time.Now()}}
	app.auctionRejectedTick = 3
	app.auctionQueueSource = "pvf"
	app.ceraRejected = map[uint32]string{2675345: "cera_unlanded"}
	app.ceraRejectedAt = map[uint32]time.Time{2675345: time.Now()}

	result, err := app.ClearSystemMarketStock()
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 9 {
		t.Fatalf("deleted = %d, want 9", result.Deleted)
	}
	if repo.creatureCount != 0 {
		t.Fatalf("creature count = %d, want 0", repo.creatureCount)
	}
	if len(app.auctionQueue) != 0 || len(app.auctionSpecialQueue) != 0 || len(app.auctionRejected) != 0 || app.auctionRejectedTick != 0 || app.auctionQueueSource != "" {
		t.Fatalf("queues not reset: queue=%v special=%v rejected=%v tick=%d source=%q", app.auctionQueue, app.auctionSpecialQueue, app.auctionRejected, app.auctionRejectedTick, app.auctionQueueSource)
	}
	if len(app.auctionRejectedMeta) != 0 {
		t.Fatalf("rejected meta not reset: %#v", app.auctionRejectedMeta)
	}
	if app.ceraRejectedCount() != 0 {
		t.Fatalf("cera rejected not reset")
	}
	if repo.collectCalls != 0 {
		t.Fatalf("system stock clear used collect path, calls=%d", repo.collectCalls)
	}
}

func TestRecoverAuctionRegistItemFailureClearsAuctionStock(t *testing.T) {
	app := testApp(t)
	repo := &clearStockRepository{counts: map[string]int{app.cfg.AuctionDB: 3}}
	app.repository = repo
	app.auctionQueue = []uint32{1001}
	app.auctionRejected = []uint32{1002}
	app.auctionRejectedMeta = map[uint32]auctionRejectedState{1002: {Count: 1}}

	if !app.recoverAuctionRegistItemFailure("auction.log") {
		t.Fatal("recovery returned false")
	}
	if repo.counts[app.cfg.AuctionDB] != 0 {
		t.Fatalf("auction stock count = %d, want 0", repo.counts[app.cfg.AuctionDB])
	}
	if len(app.auctionQueue) != 0 || len(app.auctionRejected) != 0 || len(app.auctionRejectedMeta) != 0 {
		t.Fatalf("auction queues were not reset: queue=%v rejected=%v meta=%v", app.auctionQueue, app.auctionRejected, app.auctionRejectedMeta)
	}
}
