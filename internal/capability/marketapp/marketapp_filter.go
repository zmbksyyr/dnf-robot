package marketapp

import "strings"

func marketCandidate(item catalogItem) bool {
	return item.ItemID != 0 && item.Kind != "blocked" && !isAvatarEquipment(item) && (specialAuctionKind(item) != "" || !isRiskyPVFItem(item))
}

func (a *App) marketCandidate(item catalogItem) bool {
	return marketCandidate(item) && a.marketListingAllowed(item)
}

func (a *App) marketListingAllowed(item catalogItem) bool {
	return marketListingAllowedWithConfig(item, a.configSnapshot())
}

func marketListingAllowedWithConfig(item catalogItem, cfg Config) bool {
	if item.ItemID == 0 || item.Kind == "blocked" || isAvatarEquipment(item) || !marketRarityAllowedWithConfig(item, cfg) {
		return false
	}
	if item.Kind == "equipment" {
		if cfg.Restock.EquipmentTradePolicy == tradePolicyStrict && tradeFieldBlocked(item) {
			return false
		}
		if min := cfg.Restock.EquipmentLevelMin; min > 0 && item.Level < min {
			return false
		}
		if max := cfg.Restock.EquipmentLevelMax; max > 0 && item.Level > max {
			return false
		}
		return true
	}
	return cfg.Restock.MaterialTradePolicy != tradePolicyStrict || !tradeFieldBlocked(item)
}

func tradeFieldBlocked(item catalogItem) bool {
	return item.NoTrade || (item.CanTrade != nil && !*item.CanTrade) || (item.CanAuction != nil && !*item.CanAuction)
}

func (a *App) qualityFilterEnabled() bool {
	if a == nil {
		return true
	}
	return qualityFilterEnabled(a.configSnapshot())
}

func qualityFilterEnabled(cfg Config) bool {
	return cfg.Restock.AllowedRarities != "0123456789" ||
		cfg.Restock.EquipmentTradePolicy == tradePolicyStrict ||
		cfg.Restock.MaterialTradePolicy == tradePolicyStrict
}

func (a *App) marketRarityAllowed(item catalogItem) bool {
	if a == nil {
		return item.Rarity >= 0 && item.Rarity <= 9 && strings.ContainsRune(defaultAllowedRarities, rune('0'+item.Rarity))
	}
	return marketRarityAllowedWithConfig(item, a.configSnapshot())
}

func marketRarityAllowedWithConfig(item catalogItem, cfg Config) bool {
	if item.Rarity < 0 || item.Rarity > 9 {
		return false
	}
	return strings.ContainsRune(cfg.Restock.AllowedRarities, rune('0'+item.Rarity))
}

func specialAuctionKind(item catalogItem) string {
	if item.Kind != "equipment" {
		return ""
	}
	slot := strings.ToLower(strings.TrimSpace(item.Slot))
	switch {
	case item.ItemType == 2 || slot == "titlename" || slot == "title" || slot == "title name":
		return "title"
	case item.ItemType == 30 || slot == "creature":
		return "creature"
	case slot == "artifact red" || slot == "artifact blue" || slot == "artifact green":
		return slot
	default:
		return ""
	}
}

func specialAuctionRank(item catalogItem) int {
	switch specialAuctionKind(item) {
	case "artifact red", "artifact blue", "artifact green":
		return 0
	case "creature":
		return 1
	case "title":
		return 2
	default:
		return 9
	}
}

func (a *App) catalogAuctionCandidateCounts(catalog map[uint32]catalogItem, allowed map[uint32]bool) (normal int, special int) {
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		if !a.marketCandidate(item) {
			continue
		}
		if specialAuctionKind(item) != "" {
			special++
			continue
		}
		normal++
	}
	return normal, special
}

func isAvatarEquipment(item catalogItem) bool {
	if item.Kind != "equipment" {
		return false
	}
	slot := strings.ToLower(strings.TrimSpace(item.Slot))
	return item.ItemType >= 20 && item.ItemType <= 29 || strings.Contains(slot, "avatar")
}

func isRiskyPVFItem(item catalogItem) bool {
	if isKnownZeroSuccessEquipment(item) {
		return true
	}
	switch item.ItemType {
	case 2, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30:
		return true
	default:
		return false
	}
}

func isKnownZeroSuccessEquipment(item catalogItem) bool {
	if item.Kind != "equipment" {
		return false
	}
	attach := strings.ToLower(strings.TrimSpace(item.Attach))
	slot := strings.ToLower(strings.TrimSpace(item.Slot))
	if attach == "" {
		return true
	}
	if attach != "free" {
		return false
	}
	switch slot {
	case "coatavatar", "hairavatar", "pantsavatar", "hatavatar", "faceavatar", "breastavatar", "shoesavatar", "creature":
		return true
	default:
		return false
	}
}
