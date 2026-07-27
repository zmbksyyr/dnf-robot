package marketapp

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
