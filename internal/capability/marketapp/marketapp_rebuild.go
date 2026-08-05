package marketapp

import (
	"fmt"
	"strings"
)

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
	if req.AllowedRarities != nil {
		allowed, err := normalizeAllowedRarities(*req.AllowedRarities)
		if err != nil {
			return Config{}, err
		}
		cfg.Restock.AllowedRarities = allowed
	}
	if req.EquipmentTradePolicy != nil {
		cfg.Restock.EquipmentTradePolicy = strings.TrimSpace(*req.EquipmentTradePolicy)
	}
	if req.MaterialTradePolicy != nil {
		cfg.Restock.MaterialTradePolicy = strings.TrimSpace(*req.MaterialTradePolicy)
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
	if err := writeMarketConfig(a.configPath, cfg); err != nil {
		return Config{}, err
	}
	a.setConfig(cfg)
	return cfg, nil
}
