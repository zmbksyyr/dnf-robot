package marketapp

import (
	"errors"
	"testing"
	"time"
)

func TestEnsureMarketTablesStopsAfterSuccess(t *testing.T) {
	app := testApp(t)
	repository := &clearStockRepository{ensureTables: []string{"auction", "cera"}}
	app.repository = repository
	now := time.Now()

	app.ensureMarketTables(now)
	app.ensureMarketTables(now.Add(time.Hour))

	if repository.ensureCalls != 1 {
		t.Fatalf("EnsureMarketTables calls = %d, want 1", repository.ensureCalls)
	}
	if !app.dbInitOK || app.dbInitErr != "" || len(app.dbInit) != 2 {
		t.Fatalf("unexpected db init state: ok=%t err=%q tables=%v", app.dbInitOK, app.dbInitErr, app.dbInit)
	}
}

func TestEnsureMarketTablesRetriesFailuresAtBoundedInterval(t *testing.T) {
	app := testApp(t)
	repository := &clearStockRepository{ensureErr: errors.New("database unavailable")}
	app.repository = repository
	now := time.Now()

	app.ensureMarketTables(now)
	app.ensureMarketTables(now.Add(marketTableRetryInterval / 2))
	if repository.ensureCalls != 1 {
		t.Fatalf("EnsureMarketTables calls before retry = %d, want 1", repository.ensureCalls)
	}

	repository.ensureErr = nil
	app.ensureMarketTables(now.Add(marketTableRetryInterval))
	if repository.ensureCalls != 2 {
		t.Fatalf("EnsureMarketTables calls after retry = %d, want 2", repository.ensureCalls)
	}
	if !app.dbInitOK || app.dbInitErr != "" {
		t.Fatalf("retry did not publish success: ok=%t err=%q", app.dbInitOK, app.dbInitErr)
	}
}
