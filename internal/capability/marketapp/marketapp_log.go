package marketapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"robot/internal/foundation/logfile"
)

const (
	defaultMarketLogMaxSizeMB = 100
	defaultMarketLogBackups   = 5
)

const (
	marketLogStatusActive           = "active"
	marketLogStatusBlocked          = "blocked"
	marketLogStatusClean            = "clean"
	marketLogStatusCountAfterFailed = "count_after_failed"
	marketLogStatusCountFailed      = "count_failed"
	marketLogStatusDBDeleted        = "db_deleted"
	marketLogStatusDeleteFailed     = "delete_failed"
	marketLogStatusDisabled         = "disabled"
	marketLogStatusEmpty            = "empty"
	marketLogStatusExists           = "exists"
	marketLogStatusFailed           = "failed"
	marketLogStatusFallback         = "fallback"
	marketLogStatusGameDown         = "game_down"
	marketLogStatusInstalled        = "installed"
	marketLogStatusKilled           = "killed"
	marketLogStatusQueueReset       = "queue_reset"
	marketLogStatusRestart          = "restart"
	marketLogStatusServiceDown      = "service_down"
	marketLogStatusSkipped          = "skipped"
	marketLogStatusStart            = "start"
	marketLogStatusStopSkipped      = "stop_skipped"
	marketLogStatusStopped          = "stopped"
	marketLogStatusSuccess          = "success"
	marketLogStatusSynced           = "synced"
	marketLogStatusStaleItemInfo    = "stale_iteminfo_restart"
	marketLogStatusWaitFailed       = "wait_failed"
)

type LogEvent struct {
	Time          time.Time         `json:"time"`
	Type          string            `json:"type"`
	JobID         string            `json:"job_id,omitempty"`
	Status        string            `json:"status,omitempty"`
	Market        string            `json:"market,omitempty"`
	ItemID        uint32            `json:"item_id,omitempty"`
	AuctionID     uint64            `json:"auction_id,omitempty"`
	OK            *bool             `json:"ok,omitempty"`
	Reason        interface{}       `json:"reason,omitempty"`
	Message       string            `json:"message,omitempty"`
	Summary       *PlanSummary      `json:"summary,omitempty"`
	ActionSummary *ActionLogSummary `json:"action_summary,omitempty"`
}

type ActionLogSummary struct {
	Total      int             `json:"total"`
	OK         int             `json:"ok"`
	Failed     int             `json:"failed"`
	ErrorCount int             `json:"error_count,omitempty"`
	ByMarket   map[string]int  `json:"by_market,omitempty"`
	ByReason   map[string]int  `json:"by_reason,omitempty"`
	TopFailed  []ActionLogItem `json:"top_failed,omitempty"`
}

type ActionLogItem struct {
	ItemID uint32 `json:"item_id"`
	Count  int    `json:"count"`
	Reason string `json:"reason,omitempty"`
}

const marketLogFile = "market_log.jsonl"

func marketLogPath(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, marketLogFile)
}

func (a *App) appendLog(event LogEvent) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	path := marketLogPath(a.configDir)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	maxBytes, backups := a.logLimits()
	_ = logfile.Append(path, append(data, '\n'), maxBytes, backups)
}

func (a *App) logLimits() (int64, int) {
	maxBytes := a.logMaxBytes
	if maxBytes <= 0 {
		maxBytes = int64(defaultMarketLogMaxSizeMB) * 1024 * 1024
	}
	backups := a.logBackups
	if backups <= 0 {
		backups = defaultMarketLogBackups
	}
	return maxBytes, backups
}
