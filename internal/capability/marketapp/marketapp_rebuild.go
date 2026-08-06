package marketapp

import (
	"fmt"
	"strings"
	"time"
)

// StartListingConfigRebuild accepts the request immediately and performs the
// destructive clear/restart/restock sequence in the background. Progress is
// exposed through Status.LastJob.
func (a *App) StartListingConfigRebuild(req ConfigUpdateRequest) (JobSummary, error) {
	if !a.rebuildRunning.CompareAndSwap(false, true) {
		job := busyMarketJob("listing_rebuild")
		return job, fmt.Errorf(job.Error)
	}
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("listing-rebuild-%d", start.UnixNano()),
		Kind:      "listing_rebuild",
		Status:    MarketJobStatusRunning,
		StartedAt: start,
	}
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	go func() {
		defer a.rebuildRunning.Store(false)
		_, err := a.ApplyListingConfigAndRebuild(req)
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		if err != nil {
			job.Status = MarketJobStatusFailed
			job.Error = err.Error()
		} else {
			job.Status = MarketJobStatusSuccess
		}
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error})
	}()
	return job, nil
}

// ApplyListingConfigAndRebuild applies only user-facing listing settings.
// It uses the currently deployed iteminfo.dat as the candidate boundary and
// never exports, releases, or modifies ItemInfo.
func (a *App) ApplyListingConfigAndRebuild(req ConfigUpdateRequest) (MarketRebuildResult, error) {
	itemInfoIDs, path, err := a.currentItemInfoIDs()
	if err != nil {
		return MarketRebuildResult{}, fmt.Errorf("current iteminfo %s: %w", path, err)
	}
	if len(itemInfoIDs) == 0 {
		return MarketRebuildResult{}, fmt.Errorf("current iteminfo %s contains no item ids", path)
	}

	wasAutoRunning := a.AutoRunning()
	if wasAutoRunning {
		a.StopAuto()
		defer a.StartAuto()
	}

	running := a.marketServiceRunningStates()
	serviceStates := map[string]bool{marketServiceNameAuction: running[marketServiceNameAuction]}
	a.jobMu.Lock()
	cfg, err := a.applyListingConfigLocked(req)
	if err != nil {
		a.jobMu.Unlock()
		return MarketRebuildResult{}, err
	}
	cleared, clearErr := a.clearSystemAuctionStockLocked("market_settings_apply")
	if clearErr == nil {
		a.resetAuctionQueuesLockedSafe()
	}
	a.jobMu.Unlock()
	if clearErr != nil {
		return MarketRebuildResult{Config: a.Status(), Cleared: cleared}, clearErr
	}
	if err := a.restartMarketServicesAfterItemInfo(serviceStates); err != nil {
		return MarketRebuildResult{Config: a.Status(), Cleared: cleared}, err
	}

	result := MarketRebuildResult{Cleared: cleared}
	job, err := a.RestockOnce(RestockRequest{
		Market: marketNameAuction, Execute: true,
		MaxActions: cfg.Restock.MaxActions, MaxConcurrent: cfg.Restock.MaxConcurrent,
		ContinueOnError: cfg.Auto.ContinueOnError,
	})
	result.Restocks = append(result.Restocks, job)
	result.Config = a.Status()
	return result, err
}

func (a *App) clearSystemAuctionStockLocked(logType string) (ClearSystemStockResult, error) {
	cfg := a.configSnapshot()
	result := ClearSystemStockResult{}
	item, err := a.deleteSystemMarketStock(logType, marketNameAuction, cfg.AuctionDB)
	result.Markets = append(result.Markets, item)
	result.Deleted += item.Deleted
	if err != nil {
		return result, err
	}
	item, err = a.deleteSystemCreatureItems(logType)
	result.Markets = append(result.Markets, item)
	result.Deleted += item.Deleted
	return result, err
}

func (a *App) resetAuctionQueuesLockedSafe() {
	a.stateMu.Lock()
	a.resetAuctionQueuesLocked()
	a.stateMu.Unlock()
}

func (a *App) applyListingConfigLocked(req ConfigUpdateRequest) (Config, error) {
	cfg := cloneConfig(a.configSnapshot())
	if req.EquipmentAllowedRarities != nil {
		allowed, err := normalizeAllowedRarities(*req.EquipmentAllowedRarities)
		if err != nil {
			return Config{}, err
		}
		cfg.Restock.EquipmentAllowedRarities = allowed
	}
	if req.OtherAllowedRarities != nil {
		allowed, err := normalizeAllowedRarities(*req.OtherAllowedRarities)
		if err != nil {
			return Config{}, err
		}
		cfg.Restock.OtherAllowedRarities = allowed
	}
	if req.EquipmentTradePolicy != nil {
		cfg.Restock.EquipmentTradePolicy = strings.TrimSpace(*req.EquipmentTradePolicy)
	}
	if req.OtherTradePolicy != nil {
		cfg.Restock.OtherTradePolicy = strings.TrimSpace(*req.OtherTradePolicy)
	}
	if req.BlockedItemIDExpression != nil {
		blocked, err := decodeBlockedItemIDs(*req.BlockedItemIDExpression)
		if err != nil {
			return Config{}, err
		}
		cfg.Restock.BlockedItemIDs = blocked
	} else if req.BlockedItemIDs != nil {
		cfg.Restock.BlockedItemIDs = normalizeBlockedItemIDs(req.BlockedItemIDs)
	}
	if req.StackSizes != nil {
		cfg.Restock.StackSizes = append([]int(nil), req.StackSizes...)
	}
	if req.EquipmentQtyMin != nil {
		cfg.Restock.EquipmentQtyMin = *req.EquipmentQtyMin
	}
	if req.EquipmentQtyMax != nil {
		cfg.Restock.EquipmentQtyMax = *req.EquipmentQtyMax
	}
	if req.EquipmentLevelMin != nil {
		cfg.Restock.EquipmentLevelMin = *req.EquipmentLevelMin
	}
	if req.EquipmentLevelMax != nil {
		cfg.Restock.EquipmentLevelMax = *req.EquipmentLevelMax
	}
	if req.EquipInflateMin != nil {
		cfg.Restock.EquipInflateMin = *req.EquipInflateMin
	}
	if req.EquipInflateMax != nil {
		cfg.Restock.EquipInflateMax = *req.EquipInflateMax
	}
	if req.UpgradeMin != nil {
		cfg.Restock.UpgradeMin = *req.UpgradeMin
	}
	if req.UpgradeMax != nil {
		cfg.Restock.UpgradeMax = *req.UpgradeMax
	}
	if req.UpgradePriceRate != nil {
		cfg.Restock.UpgradePriceRate = *req.UpgradePriceRate
	}
	if req.RandLow != nil {
		cfg.Restock.RandLow = *req.RandLow
	}
	if req.RandHigh != nil {
		cfg.Restock.RandHigh = *req.RandHigh
	}
	if req.CollectorEnabled != nil {
		cfg.Collector.Enabled = *req.CollectorEnabled
	}
	if req.PriceRangeEnabled != nil {
		cfg.Collector.PriceRangeEnabled = *req.PriceRangeEnabled
	}
	if req.InRangeProbability != nil {
		cfg.Collector.InRangeProbability = *req.InRangeProbability
	}
	if req.OutRangeProbability != nil {
		cfg.Collector.OutRangeProbability = *req.OutRangeProbability
	}
	if err := writeMarketConfig(a.configPath, cfg); err != nil {
		return Config{}, err
	}
	a.setConfig(cfg)
	return cfg, nil
}
