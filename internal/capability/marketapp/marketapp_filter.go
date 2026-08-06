package marketapp

import (
	"sort"
	"strings"
)

func marketCandidate(item catalogItem) bool {
	return item.ItemID != 0 && item.Kind != "blocked" && !isAvatarEquipment(item) && (specialAuctionKind(item) != "" || !isRiskyPVFItem(item))
}

func (a *App) marketCandidate(item catalogItem) bool {
	cfg := a.configSnapshot()
	return marketCandidate(item) && marketListingAllowedWithConfig(item, cfg)
}

func (a *App) marketListingAllowed(item catalogItem) bool {
	return marketListingAllowedWithConfig(item, a.configSnapshot())
}

func marketListingAllowedWithConfig(item catalogItem, cfg Config) bool {
	if item.ItemID == 0 || blockedAuctionItemWithConfig(item.ItemID, cfg) || item.Kind == "blocked" || isAvatarEquipment(item) || !marketRarityAllowedWithConfig(item, cfg) {
		return false
	}
	if item.Kind == "equipment" {
		if cfg.Restock.EquipmentTradePolicy == tradePolicyStrict && strictTradeBlocked(item) {
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
	return cfg.Restock.OtherTradePolicy != tradePolicyStrict || !strictTradeBlocked(item)
}

func strictTradeBlocked(item catalogItem) bool {
	attach := strings.ToLower(strings.TrimSpace(item.Attach))
	if item.NoTrade || (item.CanTrade != nil && !*item.CanTrade) || (item.CanAuction != nil && !*item.CanAuction) {
		return true
	}
	// Reverse-engineered from df_game_r CTradeSpace::_IsTradable @ 0x08529DCE
	// and CItem::GetAttachType @ 0x080F12E2: 0=free, 1=trade,
	// 2=trade delete, 3=sealing, 4=trade limit, 5=account. Native trading
	// allows free, and sealing only when Inven_Item byte 0 is 1; it rejects
	// 1/2/4/5. UpgradeSeparateInfo::IsTradeRestriction @ 0x08110B0A also
	// rejects bit 0x20 at Inven_Item+0x33. Robot-generated rows leave that
	// instance bit clear, so catalog filtering only needs to mirror attach.
	// Direct auction registration bypasses this native check entirely.
	if item.Kind == "equipment" {
		if attach == "free" {
			return false
		}
		// Normal sealed equipment is emitted with item type 1. Special auction
		// records use their own item type (title/creature/artifact), so they
		// cannot safely use the sealing exception.
		return attach != "sealing" || specialAuctionKind(item) != ""
	}
	// Stackable strict mode has no safe per-instance sealing state; only the
	// unbound PVF attach type is guaranteed to remain tradeable.
	return attach != "free"
}

func (a *App) qualityFilterEnabled() bool {
	if a == nil {
		return true
	}
	return qualityFilterEnabled(a.configSnapshot())
}

func qualityFilterEnabled(cfg Config) bool {
	return len(cfg.Restock.BlockedItemIDs) > 0 ||
		cfg.Restock.EquipmentAllowedRarities != "0123456789" ||
		cfg.Restock.OtherAllowedRarities != "0123456789" ||
		cfg.Restock.EquipmentTradePolicy == tradePolicyStrict ||
		cfg.Restock.OtherTradePolicy == tradePolicyStrict
}

func blockedAuctionItemWithConfig(itemID uint32, cfg Config) bool {
	ids := cfg.Restock.BlockedItemIDs
	index := sort.Search(len(ids), func(index int) bool { return ids[index] >= itemID })
	return index < len(ids) && ids[index] == itemID
}

func (a *App) marketRarityAllowed(item catalogItem) bool {
	if a == nil {
		return item.Rarity >= 0 && item.Rarity <= 9 && strings.ContainsRune(defaultEquipmentRarities, rune('0'+item.Rarity))
	}
	return marketRarityAllowedWithConfig(item, a.configSnapshot())
}

func marketRarityAllowedWithConfig(item catalogItem, cfg Config) bool {
	if item.Rarity < 0 || item.Rarity > 9 {
		return false
	}
	allowed := cfg.Restock.OtherAllowedRarities
	if item.Kind != "stackable" {
		allowed = cfg.Restock.EquipmentAllowedRarities
	}
	return strings.ContainsRune(allowed, rune('0'+item.Rarity))
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
	if isClientIncompatibleEquipment(item) {
		return true
	}
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

func isClientIncompatibleEquipment(item catalogItem) bool {
	return item.Kind == "equipment" && item.ClientIncompatible
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
