package marketapp

import (
	"fmt"
	"math"
	"strings"
)

const (
	marketConfigMaxActions       = 1_000_000
	marketConfigMaxConcurrent    = 256
	marketConfigMaxQuantity      = 10_000
	marketConfigMaxLevel         = 999
	marketConfigMaxUpgrade       = 31
	marketConfigMaxInflate       = 1_000
	marketConfigMaxRate          = 10
	marketConfigMaxRand          = 100
	marketConfigMaxDelayMS       = 60_000
	marketConfigMaxScheduleMS    = 24 * 60 * 60 * 1000
	marketConfigMinIntervalMS    = 60 * 1000
	marketConfigMaxResultActions = 100_000
)

func validateMarketConfig(cfg Config) error {
	for key, value := range map[string]string{
		"database.game_db":    cfg.GameDB,
		"database.auction_db": cfg.AuctionDB,
		"database.cera_db":    cfg.CeraDB,
	} {
		if !mysqlIdentifierPattern.MatchString(value) {
			return fmt.Errorf("%s must be a non-empty SQL identifier", key)
		}
	}
	if len(cfg.ItemInfoTargets) == 0 {
		return fmt.Errorf("iteminfo.targets must contain at least one path")
	}
	targets := make(map[string]struct{}, len(cfg.ItemInfoTargets))
	for _, target := range cfg.ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			return fmt.Errorf("iteminfo.targets must not contain empty paths")
		}
		if _, exists := targets[target]; exists {
			return fmt.Errorf("iteminfo.targets contains duplicate path %q", target)
		}
		targets[target] = struct{}{}
	}
	if cfg.SystemOwner.IDBase == 0 || cfg.SystemOwner.BuyerBase == 0 {
		return fmt.Errorf("system_owner id_base and buyer_base must be positive")
	}
	if strings.TrimSpace(cfg.SystemOwner.OwnerName) == "" || strings.TrimSpace(cfg.SystemOwner.CeraName) == "" {
		return fmt.Errorf("system_owner names must not be empty")
	}
	if cfg.SystemOwner.RotateEvery <= 0 {
		return fmt.Errorf("system_owner.rotate_every must be positive")
	}

	if err := validateMarketLimits("auction_collect", cfg.Collector.MaxActions, cfg.Collector.MaxConcurrent); err != nil {
		return err
	}
	if err := validateProbability("auction_collect.in_range_probability", cfg.Collector.InRangeProbability); err != nil {
		return err
	}
	if err := validateProbability("auction_collect.out_of_range_probability", cfg.Collector.OutRangeProbability); err != nil {
		return err
	}

	r := cfg.Restock
	allowedRarities, err := normalizeAllowedRarities(r.AllowedRarities)
	if err != nil {
		return err
	}
	if allowedRarities != r.AllowedRarities {
		return fmt.Errorf("auction_price.allowed_rarities must be sorted and contain no duplicates")
	}
	if r.EquipmentTradePolicy != tradePolicyPermissive && r.EquipmentTradePolicy != tradePolicyStrict {
		return fmt.Errorf("auction_price.equipment_trade_policy must be permissive or strict")
	}
	if r.MaterialTradePolicy != tradePolicyPermissive && r.MaterialTradePolicy != tradePolicyStrict {
		return fmt.Errorf("auction_price.material_trade_policy must be permissive or strict")
	}
	if len(r.StackSizes) == 0 {
		return fmt.Errorf("auction_price.stack_sizes must contain at least one value")
	}
	seenStack := make(map[int]struct{}, len(r.StackSizes))
	for _, size := range r.StackSizes {
		if size <= 0 || int64(size) > int64(maxInt32) {
			return fmt.Errorf("auction_price.stack_sizes values must be in 1..%d", maxInt32)
		}
		if _, exists := seenStack[size]; exists {
			return fmt.Errorf("auction_price.stack_sizes contains duplicate value %d", size)
		}
		seenStack[size] = struct{}{}
	}
	if r.EquipmentQtyMin <= 0 || r.EquipmentQtyMin > marketConfigMaxQuantity || r.EquipmentQtyMax < r.EquipmentQtyMin || r.EquipmentQtyMax > marketConfigMaxQuantity {
		return fmt.Errorf("auction_price equipment quantity must satisfy 1 <= min <= max <= %d", marketConfigMaxQuantity)
	}
	if r.EquipmentLevelMin < 0 || r.EquipmentLevelMin > marketConfigMaxLevel || r.EquipmentLevelMax < 0 || r.EquipmentLevelMax > marketConfigMaxLevel || r.EquipmentLevelMax > 0 && r.EquipmentLevelMax < r.EquipmentLevelMin {
		return fmt.Errorf("auction_price equipment levels must satisfy 0 <= min <= max (0 max means unlimited)")
	}
	if r.EquipInflateMin <= 0 || r.EquipInflateMin > marketConfigMaxInflate || r.EquipInflateMax < r.EquipInflateMin || r.EquipInflateMax > marketConfigMaxInflate {
		return fmt.Errorf("auction_price equipment multipliers must satisfy 1 <= min <= max <= %d", marketConfigMaxInflate)
	}
	if r.UpgradeMin < 0 || r.UpgradeMin > marketConfigMaxUpgrade || r.UpgradeMax < r.UpgradeMin || r.UpgradeMax > marketConfigMaxUpgrade {
		return fmt.Errorf("auction_price upgrades must satisfy 0 <= min <= max <= %d", marketConfigMaxUpgrade)
	}
	if !finiteInRange(r.UpgradePriceRate, 0, marketConfigMaxRate) {
		return fmt.Errorf("auction_price.upgrade_price_rate must be finite and in 0..%d", marketConfigMaxRate)
	}
	if !finiteInRange(r.RandLow, 0, marketConfigMaxRand) || r.RandLow <= 0 || !finiteInRange(r.RandHigh, 0, marketConfigMaxRand) || r.RandHigh < r.RandLow {
		return fmt.Errorf("auction_price random multipliers must be finite, positive, and satisfy low <= high")
	}
	if err := validateMarketLimits("auction_price", r.MaxActions, r.MaxConcurrent); err != nil {
		return err
	}
	if r.MaxResultActions <= 0 || r.MaxResultActions > marketConfigMaxResultActions {
		return fmt.Errorf("auction_price.max_result_actions must be in 1..%d", marketConfigMaxResultActions)
	}
	if r.PerItemDelayMS < 0 || r.PerItemDelayMS > marketConfigMaxDelayMS {
		return fmt.Errorf("auction_price.per_item_delay_ms must be in 0..%d", marketConfigMaxDelayMS)
	}

	if err := validateCeraItems(cfg.Cera.Items); err != nil {
		return err
	}
	if len(cfg.Auto.Markets) == 0 {
		return fmt.Errorf("auto.markets must contain at least one market")
	}
	seenMarkets := make(map[string]struct{}, len(cfg.Auto.Markets))
	for _, market := range cfg.Auto.Markets {
		if market != marketNameAuction && market != marketNameCera {
			return fmt.Errorf("auto.markets contains unsupported market %q", market)
		}
		if _, exists := seenMarkets[market]; exists {
			return fmt.Errorf("auto.markets contains duplicate market %q", market)
		}
		seenMarkets[market] = struct{}{}
	}
	if cfg.Auto.InitialDelayMS < 0 || cfg.Auto.InitialDelayMS > marketConfigMaxScheduleMS {
		return fmt.Errorf("auto.initial_delay_ms must be in 0..%d", marketConfigMaxScheduleMS)
	}
	if cfg.Auto.IntervalMS < marketConfigMinIntervalMS || cfg.Auto.IntervalMS > marketConfigMaxScheduleMS {
		return fmt.Errorf("auto.interval_ms must be in %d..%d", marketConfigMinIntervalMS, marketConfigMaxScheduleMS)
	}
	if err := validateMarketLimits("auto", cfg.Auto.MaxActions, cfg.Auto.MaxConcurrent); err != nil {
		return err
	}
	return nil
}

func normalizeAllowedRarities(value string) (string, error) {
	value = strings.TrimSpace(value)
	var seen [10]bool
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("auction_price.allowed_rarities must contain only digits 0..9")
		}
		seen[digit-'0'] = true
	}
	var normalized strings.Builder
	for digit, blocked := range seen {
		if blocked {
			normalized.WriteByte(byte('0' + digit))
		}
	}
	return normalized.String(), nil
}

func validateMarketLimits(section string, maxActions, maxConcurrent int) error {
	if maxActions < 0 || maxActions > marketConfigMaxActions {
		return fmt.Errorf("%s.max_actions must be in 0..%d", section, marketConfigMaxActions)
	}
	if maxConcurrent <= 0 || maxConcurrent > marketConfigMaxConcurrent {
		return fmt.Errorf("%s.max_concurrent must be in 1..%d", section, marketConfigMaxConcurrent)
	}
	return nil
}

func validateProbability(key string, value float64) error {
	if !finiteInRange(value, 0, 1) {
		return fmt.Errorf("%s must be finite and in 0..1", key)
	}
	return nil
}

func finiteInRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}

func validateCeraItems(items []ceraRow) error {
	if len(items) == 0 {
		return fmt.Errorf("cera.items must contain at least one entry")
	}
	seen := make(map[uint32]struct{}, len(items))
	for index, row := range items {
		prefix := fmt.Sprintf("cera.items[%d]", index)
		if row.ItemID == 0 {
			return fmt.Errorf("%s.item_id must be positive", prefix)
		}
		if strings.TrimSpace(row.Label) == "" {
			return fmt.Errorf("%s.name must not be empty", prefix)
		}
		if row.RestockPrice <= 0 || int64(row.RestockPrice) > int64(maxInt32) {
			return fmt.Errorf("%s.restock_price must be in 1..%d", prefix, maxInt32)
		}
		if row.RestockQty <= 0 || row.RestockQty > marketConfigMaxQuantity {
			return fmt.Errorf("%s.restock_qty must be in 1..%d even when disabled", prefix, marketConfigMaxQuantity)
		}
		if _, exists := seen[row.ItemID]; exists {
			return fmt.Errorf("cera.items contains duplicate item_id %d", row.ItemID)
		}
		seen[row.ItemID] = struct{}{}
	}
	return nil
}
