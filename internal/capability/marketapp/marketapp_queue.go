package marketapp

const auctionRejectedRetryEvery = 10
const auctionRejectedRetryDivisor = 100
const auctionSpecialBudgetDivisor = 10

func (a *App) targetAuctionSelection(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, itemIDs []uint32) (auctionQueueSelection, error) {
	itemInfo := map[uint32]itemInfoEntry(nil)
	if pvfReady {
		var err error
		itemInfo, _, err = a.currentItemInfoEntries()
		if err != nil {
			a.appendLog(LogEvent{Type: "iteminfo_gate", Status: marketLogStatusBlocked, Message: err.Error()})
			return auctionQueueSelection{}, nil
		}
	}

	rows := make([]restockRow, 0, len(itemIDs))
	selected := auctionQueueCounts{}
	seen := map[uint32]bool{}
	for _, id := range itemIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if have[id] > 0 {
			continue
		}
		entry, itemInfoAllowed := itemInfo[id]
		if itemInfo != nil && !itemInfoAllowed {
			continue
		}
		row, ok := a.auctionRowForID(pvfReady, catalog, id)
		if !ok {
			continue
		}
		if itemInfoAllowed && shouldApplyItemInfoType(row, entry) {
			row.ItemType = entry.ItemType
		}
		rows = append(rows, row)
		if specialAuctionKind(row.marketItem()) != "" {
			selected.Special++
		} else {
			selected.Normal++
		}
	}
	return auctionQueueSelection{Rows: rows, Selected: selected}, nil
}

func shouldApplyItemInfoType(row restockRow, entry itemInfoEntry) bool {
	if entry.ItemType < 0 {
		return false
	}
	if row.Kind == "equipment" && row.ItemType > 0 && specialAuctionKind(row.marketItem()) == "" {
		return false
	}
	return true
}

func (a *App) nextAuctionQueueSelection(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int) (auctionQueueSelection, error) {
	if a.auctionQueueNeedsReload(pvfReady) {
		if err := a.reloadAuctionQueues(pvfReady, catalog); err != nil {
			return auctionQueueSelection{}, err
		}
	}

	a.stateMu.Lock()
	a.reconcileRejectedStockLocked(have, pvfReady, catalog)
	budget := a.auctionQueueBudgetLocked(maxActions)
	rows, selected := a.selectAuctionQueueRowsLocked(pvfReady, catalog, have, maxActions, budget)
	a.stateMu.Unlock()
	return auctionQueueSelection{Rows: rows, Budget: budget, Selected: selected}, nil
}

func (a *App) auctionQueueNeedsReload(pvfReady bool) bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return len(a.auctionQueue) == 0 && len(a.auctionSpecialQueue) == 0 || pvfReady && a.auctionQueueSource != marketQueueSourcePVFItemInfo
}

func (a *App) reloadAuctionQueues(pvfReady bool, catalog map[uint32]catalogItem) error {
	candidates, err := a.auctionQueueCandidates(pvfReady, catalog)
	if err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if len(a.auctionQueue) != 0 || len(a.auctionSpecialQueue) != 0 {
		if candidates.Source != marketQueueSourcePVFItemInfo || a.auctionQueueSource == marketQueueSourcePVFItemInfo {
			return nil
		}
	}
	a.applyAuctionQueueCandidatesLocked(candidates)
	return nil
}

func (a *App) applyAuctionQueueCandidatesLocked(candidates auctionQueueCandidatesResult) {
	candidateSet := idSet(append(append([]uint32{}, candidates.Normal...), candidates.Special...))
	a.auctionRejected = filterQueueBySet(a.auctionRejected, candidateSet)
	a.pruneAuctionRejectedMetaLocked()
	rejectedSet := idSet(a.auctionRejected)
	a.auctionQueue = filterQueueExcludeSet(candidates.Normal, rejectedSet)
	a.auctionSpecialQueue = filterQueueExcludeSet(candidates.Special, rejectedSet)
	a.auctionQueueSource = candidates.Source
}

func (a *App) selectAuctionQueueRowsLocked(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int, budget auctionQueueBudget) ([]restockRow, auctionQueueCounts) {
	selected := make([]restockRow, 0)
	counts := auctionQueueCounts{}
	if maxActions <= 0 || budget.Special != 0 {
		rows := a.selectAuctionRowsFromQueue(&a.auctionSpecialQueue, pvfReady, catalog, have, budget.Special, false)
		counts.Special = len(rows)
		selected = append(selected, rows...)
	}
	rows := a.selectAuctionRowsFromQueue(&a.auctionQueue, pvfReady, catalog, have, budget.Normal, false)
	counts.Normal = len(rows)
	selected = append(selected, rows...)
	if maxActions <= 0 || budget.Rejected != 0 {
		rows := a.selectAuctionRowsFromQueue(&a.auctionRejected, pvfReady, catalog, have, budget.Rejected, true)
		counts.Rejected = len(rows)
		selected = append(selected, rows...)
	}
	return selected, counts
}

func (a *App) auctionQueueBudgetLocked(maxActions int) auctionQueueBudget {
	if maxActions <= 0 {
		return auctionQueueBudget{}
	}
	rejected := 0
	if len(a.auctionRejected) == 0 {
		a.auctionRejectedTick = 0
	} else {
		a.auctionRejectedTick++
		if a.auctionRejectedTick >= auctionRejectedRetryEvery {
			a.auctionRejectedTick = 0
			rejected = maxActions / auctionRejectedRetryDivisor
			if rejected <= 0 {
				rejected = 1
			}
			if rejected > maxActions {
				rejected = maxActions
			}
		}
	}
	available := maxActions - rejected
	special := 0
	if len(a.auctionSpecialQueue) > 0 && available > 1 {
		special = available / auctionSpecialBudgetDivisor
		if special <= 0 {
			special = 1
		}
	}
	return auctionQueueBudget{Normal: available - special, Special: special, Rejected: rejected}
}

func (a *App) selectAuctionRowsFromQueue(queue *[]uint32, pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int, rejected bool) []restockRow {
	queueLen := len(*queue)
	selected := make([]restockRow, 0)
	planned := 0
	for i := 0; i < queueLen; i++ {
		id := (*queue)[0]
		*queue = (*queue)[1:]
		if have[id] > 0 {
			if rejected {
				a.appendAuctionAvailableLocked(id, pvfReady, catalog)
			} else {
				*queue = append(*queue, id)
			}
			continue
		}
		row, ok := a.auctionRowForID(pvfReady, catalog, id)
		if !ok {
			continue
		}
		records := auctionTargetRecords(row)
		if maxActions > 0 && planned > 0 && planned+records > maxActions {
			*queue = append(*queue, id)
			continue
		}
		selected = append(selected, row)
		planned += records
		*queue = append(*queue, id)
		if maxActions > 0 && planned >= maxActions {
			break
		}
	}
	return selected
}

func (a *App) auctionQueueSnapshotLocked() auctionQueueSnapshot {
	snapshot := auctionQueueSnapshot{
		Normal:          len(a.auctionQueue),
		Special:         len(a.auctionSpecialQueue),
		Rejected:        len(a.auctionRejected),
		RejectedTracked: len(a.auctionRejectedMeta),
		RejectedRetryIn: auctionRejectedRetryEvery - a.auctionRejectedTick,
		RejectedReasons: topAuctionRejectedReasons(a.auctionRejectedMeta, 5),
		Source:          a.auctionQueueSource,
	}
	if snapshot.Rejected == 0 {
		snapshot.RejectedRetryIn = 0
	}
	return snapshot
}

func idSet(ids []uint32) map[uint32]bool {
	out := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			out[id] = true
		}
	}
	return out
}

func filterQueueBySet(ids []uint32, keep map[uint32]bool) []uint32 {
	out := ids[:0]
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && keep[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func filterQueueExcludeSet(ids []uint32, exclude map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(ids))
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && !exclude[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func removeQueueID(ids []uint32, itemID uint32) []uint32 {
	out := ids[:0]
	for _, id := range ids {
		if id != itemID {
			out = append(out, id)
		}
	}
	return out
}

func queueContains(ids []uint32, itemID uint32) bool {
	for _, id := range ids {
		if id == itemID {
			return true
		}
	}
	return false
}

func auctionTargetRecords(row restockRow) int {
	item := row.marketItem()
	stackSize := auctionPlanStackSize(row, item, item.Kind == "equipment")
	return (row.Quantity + stackSize - 1) / stackSize
}
