package marketapp

func (a *App) planAuction(rows []restockRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.Quantity <= 0 || !row.Enabled {
			continue
		}
		item, ok := catalog[row.ItemID]
		if !ok {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: row.Name, Reason: "missing_from_pvf"})
			continue
		}
		if item.Name == "" {
			item.Name = row.Name
		}
		if item.Kind == "blocked" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: item.Name, Reason: "not_auctionable"})
			continue
		}
		if isRiskyPVFItem(item) {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: item.Name, Reason: "risky_special_type"})
			continue
		}
		isEquip := item.Kind == "equipment"
		if row.SealFlag != 0 && !isEquip {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: row.Name, Reason: "requires_add_info"})
			continue
		}
		stackSize := row.StackSize
		if stackSize <= 0 {
			stackSize = 1
		}
		if isEquip {
			stackSize = 1
		}
		if !isEquip && item.StackLimit > 0 && stackSize > item.StackLimit {
			stackSize = item.StackLimit
		}
		targetRecords := (row.Quantity + stackSize - 1) / stackSize
		current := have[row.ItemID]
		need := targetRecords - current
		if need <= 0 {
			continue
		}
		for i := 0; i < need; i++ {
			pos := current + i
			count := int32(1)
			if !isEquip {
				if pos < targetRecords-1 {
					count = int32(stackSize)
				} else {
					count = int32(row.Quantity - (targetRecords-1)*stackSize)
				}
			}
			unit := a.price(row.SystemPrice)
			total := unit
			addInfo := int32(0)
			if isEquip {
				addInfo = 0
			} else {
				addInfo = count
				total = unit * count
			}
			ownerID := a.pickOwner(occ)
			result.Actions = append(result.Actions, Action{
				Market:       "auction",
				Kind:         item.Kind,
				ItemID:       row.ItemID,
				Name:         item.Name,
				Count:        count,
				UnitPrice:    unit,
				TotalPrice:   total,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.OwnerName,
				CountAddInfo: addInfo,
				StartPrice:   total - 1,
				InstantPrice: total,
				Source:       a.cfg.Restock.File,
			})
		}
	}
}

func (a *App) planCera(rows []ceraRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.RestockQty <= 0 || !row.Enabled {
			continue
		}
		if _, ok := catalog[row.ItemID]; !ok {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "cera", ItemID: row.ItemID, Name: row.Label, Reason: "missing_from_pvf"})
			continue
		}
		current := have[row.ItemID]
		need := row.RestockQty - current
		for i := 0; i < need; i++ {
			ownerID := a.pickOwner(occ)
			result.Actions = append(result.Actions, Action{
				Market:       "cera",
				Kind:         "gold",
				ItemID:       row.ItemID,
				Name:         row.Label,
				Count:        1,
				UnitPrice:    row.RestockPrice,
				TotalPrice:   row.RestockPrice,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.CeraName,
				CountAddInfo: 1,
				StartPrice:   -1,
				InstantPrice: row.RestockPrice,
				Source:       a.cfg.Restock.File,
			})
		}
	}
}

func (a *App) price(base int32) int32 {
	if base <= 0 {
		base = 1
	}
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low <= 0 || high <= 0 || low == high {
		return base
	}
	v := float64(base) * (low + a.rand.Float64()*(high-low))
	if v < 1 {
		return 1
	}
	return int32(v)
}

func (a *App) pickOwner(occ map[uint32]int) uint32 {
	owner := a.cfg.SystemOwner.IDBase
	for occ[owner] >= a.cfg.SystemOwner.RotateEvery {
		owner++
	}
	occ[owner]++
	return owner
}

func isRiskyPVFItem(item catalogItem) bool {
	switch item.ItemType {
	case 2, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30:
		return true
	default:
		return false
	}
}
