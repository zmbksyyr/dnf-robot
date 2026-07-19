package marketapp

import "strings"

const defaultAuctionEquipmentEndurance = 10

type normalAuctionPlan struct {
	Row          restockRow
	Item         catalogItem
	IsEquipment  bool
	StackSize    int
	TargetRecord int
	BatchInflate float64
}

func (a *App) planAuction(rows []restockRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.Quantity <= 0 || !row.Enabled {
			continue
		}
		row, item := auctionPlanRow(row, catalog)
		if special := specialAuctionKind(item); special != "" {
			a.planSpecialAuction(row, item, special, have, occ, result)
			continue
		}
		if reason := auctionPlanSkipReason(row, item); reason != "" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: reason})
			continue
		}
		if have[row.ItemID] > 0 {
			continue
		}
		plan := a.normalAuctionPlan(row, item)
		a.appendNormalAuctionActions(plan, occ, result)
		have[row.ItemID] = plan.TargetRecord
	}
}

func auctionPlanRow(row restockRow, catalog map[uint32]catalogItem) (restockRow, catalogItem) {
	if row.Kind == "" {
		if catalogItem, ok := catalog[row.ItemID]; ok {
			row.applyMarketItem(catalogItem)
		}
	}
	item := row.marketItem()
	if item.Name == "" {
		item.Name = row.Name
	}
	return row, item
}

func auctionPlanSkipReason(row restockRow, item catalogItem) string {
	switch {
	case item.Kind == "blocked":
		return "not_auctionable"
	case isAvatarEquipment(item):
		return "avatar_not_auctionable"
	case isRiskyPVFItem(item):
		return "risky_special_type"
	case row.SealFlag != 0 && item.Kind != "equipment":
		return "requires_add_info"
	default:
		return ""
	}
}

func (a *App) normalAuctionPlan(row restockRow, item catalogItem) normalAuctionPlan {
	isEquip := item.Kind == "equipment"
	stackSize := auctionPlanStackSize(row, item, isEquip)
	plan := normalAuctionPlan{
		Row:          row,
		Item:         item,
		IsEquipment:  isEquip,
		StackSize:    stackSize,
		TargetRecord: (row.Quantity + stackSize - 1) / stackSize,
		BatchInflate: 1,
	}
	if isEquip {
		plan.BatchInflate = float64(randRange(a.rand, a.cfg.Restock.EquipInflateMin, a.cfg.Restock.EquipInflateMax))
	}
	return plan
}

func auctionPlanStackSize(row restockRow, item catalogItem, isEquip bool) int {
	stackSize := row.StackSize
	if stackSize <= 0 {
		stackSize = 1
	}
	if isEquip {
		return 1
	}
	if item.StackLimit > 0 && stackSize > item.StackLimit {
		return item.StackLimit
	}
	return stackSize
}

func (a *App) appendNormalAuctionActions(plan normalAuctionPlan, occ map[uint32]int, result *PlanResult) {
	for i := 0; i < plan.TargetRecord; i++ {
		count := auctionPlanActionCount(plan, i)
		addInfo := count
		upgrade := 0
		endurance := 0
		itemType := plan.Item.ItemType
		if plan.IsEquipment {
			addInfo = 0
			itemType = auctionEquipmentProtocolItemType(plan.Item)
			endurance = plan.Row.Endurance
			if endurance <= 0 {
				endurance = defaultAuctionEquipmentEndurance
			}
			upgrade = plan.Row.Upgrade
			if upgrade <= 0 {
				upgrade = randRange(a.rand, a.cfg.Restock.UpgradeMin, a.cfg.Restock.UpgradeMax)
			}
		}
		unit := a.auctionUnitPrice(plan.Row.SystemPrice, plan.IsEquipment, plan.BatchInflate, upgrade)
		total := unit
		if !plan.IsEquipment {
			count = safeAuctionStackCount(unit, count)
			addInfo = count
			total = safeAuctionTotalPrice(unit, count)
		}
		result.Actions = append(result.Actions, Action{
			Market:       marketNameAuction,
			Kind:         plan.Item.Kind,
			ItemID:       plan.Row.ItemID,
			ItemType:     itemType,
			Name:         plan.Item.Name,
			Count:        count,
			UnitPrice:    unit,
			TotalPrice:   total,
			OwnerID:      a.pickOwner(occ),
			OwnerName:    a.cfg.SystemOwner.OwnerName,
			CountAddInfo: addInfo,
			StartPrice:   total - 1,
			InstantPrice: total,
			Upgrade:      upgrade,
			Endurance:    endurance,
			Source:       auctionActionSource(plan.Row),
		})
	}
}

func auctionEquipmentProtocolItemType(item catalogItem) int {
	if specialAuctionKind(item) != "" {
		return item.ItemType
	}
	if auctionEquipmentShouldSeal(item) {
		return 1
	}
	return 0
}

func auctionEquipmentShouldSeal(item catalogItem) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(item.Attach)), "sealing")
}

func safeAuctionStackCount(unit int32, count int32) int32 {
	if count <= 0 {
		return 1
	}
	if unit <= 0 {
		return count
	}
	maxCount := maxInt32 / unit
	if maxCount < 1 {
		return 1
	}
	if count > maxCount {
		return maxCount
	}
	return count
}

func safeAuctionTotalPrice(unit int32, count int32) int32 {
	if unit <= 0 {
		unit = 1
	}
	if count <= 0 {
		count = 1
	}
	total := int64(unit) * int64(count)
	if total > int64(maxInt32) {
		return maxInt32
	}
	if total < 1 {
		return 1
	}
	return int32(total)
}

func auctionPlanActionCount(plan normalAuctionPlan, pos int) int32 {
	if plan.IsEquipment {
		return 1
	}
	if pos < plan.TargetRecord-1 {
		return int32(plan.StackSize)
	}
	return int32(plan.Row.Quantity - (plan.TargetRecord-1)*plan.StackSize)
}

func auctionActionSource(row restockRow) string {
	if row.Source != "" {
		return row.Source
	}
	return marketActionSourceUnknown
}

func (a *App) planSpecialAuction(row restockRow, item catalogItem, special string, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	if have[row.ItemID] > 0 {
		return
	}
	records := row.Quantity
	if records <= 0 {
		records = randRange(a.rand, a.cfg.Restock.EquipmentQtyMin, a.cfg.Restock.EquipmentQtyMax)
	}
	if records <= 0 {
		records = 1
	}
	batchInflate := float64(randRange(a.rand, a.cfg.Restock.EquipInflateMin, a.cfg.Restock.EquipInflateMax))
	planned := 0
	for i := 0; i < records; i++ {
		unit := a.auctionUnitPrice(row.SystemPrice, true, batchInflate, 0)
		ownerID := a.pickOwner(occ)
		action := Action{
			Market:       marketNameAuction,
			Kind:         special,
			ItemID:       row.ItemID,
			ItemType:     item.ItemType,
			Name:         item.Name,
			Count:        1,
			UnitPrice:    unit,
			TotalPrice:   unit,
			OwnerID:      ownerID,
			OwnerName:    a.cfg.SystemOwner.OwnerName,
			StartPrice:   unit - 1,
			InstantPrice: unit,
			Source:       row.Source,
		}
		if special == "creature" {
			uiID, err := a.repository.CreateCreatureItem(a.cfg.GameDB, ownerID, row.ItemID)
			if err != nil {
				result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameAuction, ItemID: row.ItemID, Name: item.Name, Reason: "creature_instance_failed"})
				continue
			}
			action.CountAddInfo = uiID
		} else {
			action.CountAddInfo = a.nextSpecialAddInfo()
		}
		result.Actions = append(result.Actions, action)
		planned++
	}
	if planned > 0 {
		have[row.ItemID] = planned
	}
}

// ---- cera_plan.go ----
func (a *App) planCera(rows []ceraRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	type pendingCera struct {
		row  ceraRow
		need int
	}
	pending := make([]pendingCera, 0, len(rows))
	for _, row := range rows {
		if row.ItemID == 0 || row.RestockQty <= 0 || !row.Enabled {
			continue
		}
		if reason := a.ceraRejectedReason(row.ItemID); reason != "" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameCera, ItemID: row.ItemID, Name: row.Label, Reason: reason})
			continue
		}
		if catalog != nil {
			if _, ok := catalog[row.ItemID]; !ok {
				result.Skipped = append(result.Skipped, SkippedItem{Market: marketNameCera, ItemID: row.ItemID, Name: row.Label, Reason: "missing_from_pvf"})
				continue
			}
		}
		current := have[row.ItemID]
		need := row.RestockQty - current
		if need > 0 {
			pending = append(pending, pendingCera{row: row, need: need})
		}
	}
	for {
		added := false
		for i := range pending {
			if pending[i].need <= 0 {
				continue
			}
			row := pending[i].row
			ownerID := a.pickOwner(occ)
			price := a.price(row.RestockPrice)
			result.Actions = append(result.Actions, Action{
				Market:       marketNameCera,
				Kind:         marketAliasGold,
				ItemID:       row.ItemID,
				Name:         row.Label,
				Count:        1,
				UnitPrice:    price,
				TotalPrice:   price,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.CeraName,
				CountAddInfo: 1,
				StartPrice:   -1,
				InstantPrice: price,
				Source:       marketActionSourceCeraConfig,
			})
			pending[i].need--
			added = true
		}
		if !added {
			return
		}
	}
}
