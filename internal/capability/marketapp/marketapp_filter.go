package marketapp

import "strings"

func marketCandidate(item catalogItem) bool {
	return item.ItemID != 0 && item.Kind != "blocked" && !isAvatarEquipment(item) && (specialAuctionKind(item) != "" || !isRiskyPVFItem(item))
}

func (a *App) marketCandidate(item catalogItem) bool {
	if !marketCandidate(item) {
		return false
	}
	if !a.qualityFilterEnabled() {
		return true
	}
	return marketRarityAllowed(item)
}

func (a *App) qualityFilterEnabled() bool {
	return a == nil || a.cfg.Restock.QualityFilter == nil || *a.cfg.Restock.QualityFilter
}

func marketRarityAllowed(item catalogItem) bool {
	return item.Rarity < 4
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

func catalogAuctionCandidateCounts(catalog map[uint32]catalogItem, allowed map[uint32]bool) (normal int, special int) {
	return (*App)(nil).catalogAuctionCandidateCounts(catalog, allowed)
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
