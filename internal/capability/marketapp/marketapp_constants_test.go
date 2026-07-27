package marketapp

import "testing"

func TestMarketJobStatusConstants(t *testing.T) {
	tests := map[string]string{
		MarketJobStatusBusy:          "busy",
		MarketJobStatusRunning:       "running",
		MarketJobStatusFailed:        "failed",
		MarketJobStatusPendingDB:     "pending_db_confirm",
		MarketJobStatusPlanned:       "planned",
		MarketJobStatusPartialFailed: "partial_failed",
		MarketJobStatusSuccess:       "success",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("market job status got %q want %q", got, want)
		}
	}
}

func TestMarketLogStatusConstants(t *testing.T) {
	tests := map[string]string{
		marketLogStatusActive:           "active",
		marketLogStatusBlocked:          "blocked",
		marketLogStatusClean:            "clean",
		marketLogStatusCountAfterFailed: "count_after_failed",
		marketLogStatusCountFailed:      "count_failed",
		marketLogStatusDBDeleted:        "db_deleted",
		marketLogStatusDeleteFailed:     "delete_failed",
		marketLogStatusDisabled:         "disabled",
		marketLogStatusEmpty:            "empty",
		marketLogStatusExists:           "exists",
		marketLogStatusFailed:           "failed",
		marketLogStatusFallback:         "fallback",
		marketLogStatusGameDown:         "game_down",
		marketLogStatusInstalled:        "installed",
		marketLogStatusKilled:           "killed",
		marketLogStatusQueueReset:       "queue_reset",
		marketLogStatusRestart:          "restart",
		marketLogStatusServiceDown:      "service_down",
		marketLogStatusSkipped:          "skipped",
		marketLogStatusStart:            "start",
		marketLogStatusStopSkipped:      "stop_skipped",
		marketLogStatusStopped:          "stopped",
		marketLogStatusSuccess:          "success",
		marketLogStatusSynced:           "synced",
		marketLogStatusStaleItemInfo:    "stale_iteminfo_restart",
		marketLogStatusWaitFailed:       "wait_failed",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("market log status got %q want %q", got, want)
		}
	}
}

func TestMarketServiceStatusConstants(t *testing.T) {
	tests := map[string]string{
		MarketServiceStatusReady:                   "ready",
		MarketServiceStatusDown:                    "down",
		MarketServiceStatusPortReadyProcessMissing: "port_ready_process_missing",
		MarketServiceStatusProcessWithoutPort:      "process_without_port",
		MarketServiceStatusPrepareFailed:           "prepare_failed",
		MarketServiceStatusStartFailed:             "start_failed",
		MarketServiceStatusRegistItemFailed:        "regist_item_failed",
		MarketServiceStatusProcessExited:           "process_exited",
		MarketServiceStatusPortReadyButUnstable:    "port_ready_but_unstable",
		MarketServiceStatusStartTimeout:            "start_timeout",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("market service status got %q want %q", got, want)
		}
	}
}

func TestMarketPolicyHealthConstants(t *testing.T) {
	tests := map[string]string{
		marketPolicyHealthHealthy:    "healthy",
		marketPolicyHealthRecovering: "recovering",
		marketPolicyHealthDegraded:   "degraded",
		marketPolicyHealthBlocked:    "blocked",
		marketPolicyHealthWarning:    "warning",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("market policy health got %q want %q", got, want)
		}
	}
}

func TestMarketPolicyModeConstants(t *testing.T) {
	tests := map[string]string{
		marketPolicyModeNormal:   "normal",
		marketPolicyModeRecover:  "recover",
		marketPolicyModeDegraded: "degraded",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("market policy mode got %q want %q", got, want)
		}
	}
}

func TestMarketFactConstants(t *testing.T) {
	tests := []struct {
		got  string
		want string
	}{
		{marketNameAuction, "auction"},
		{marketNameCera, "cera"},
		{marketAliasGold, "gold"},
		{marketAliasPoint, "point"},
		{marketServiceNameAuction, "auction"},
		{marketServiceNamePoint, "point"},
		{marketQueueSourcePVFItemInfo, "pvf_iteminfo"},
		{marketQueueSourcePVFItemInfoMissing, "pvf_iteminfo_missing"},
		{marketQueueSourceFallback, "fallback"},
		{marketRowSourcePVF, "pvf"},
		{marketRowSourceFallbackSeed, "fallback_seed"},
		{marketActionSourceUnknown, "unknown"},
		{marketActionSourceCeraConfig, "cera_config"},
		{marketCandidateSourceUnavailable, "unavailable"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("market fact constant got %q want %q", tt.got, tt.want)
		}
	}
}
