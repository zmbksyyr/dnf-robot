package marketapp

import (
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

const auctionRejectedRetryEvery = 10
const auctionRejectedRetryDivisor = 100
const auctionSpecialBudgetDivisor = 10
const ceraRejectedTTL = 30 * time.Minute

func (a *App) ceraRejectedReason(itemID uint32) string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.ceraRejected == nil {
		return ""
	}
	reason := a.ceraRejected[itemID]
	if reason == "" {
		return ""
	}
	if t, ok := a.ceraRejectedAt[itemID]; ok && time.Since(t) > ceraRejectedTTL {
		delete(a.ceraRejected, itemID)
		delete(a.ceraRejectedAt, itemID)
		return ""
	}
	return reason
}

func (a *App) markCeraRejected(itemID uint32, reason string) {
	if itemID == 0 {
		return
	}
	if reason == "" {
		reason = "cera_unlanded"
	}
	a.stateMu.Lock()
	if a.ceraRejected == nil {
		a.ceraRejected = map[uint32]string{}
	}
	if a.ceraRejectedAt == nil {
		a.ceraRejectedAt = map[uint32]time.Time{}
	}
	if _, exists := a.ceraRejected[itemID]; !exists {
		a.ceraRejected[itemID] = reason
	}
	a.ceraRejectedAt[itemID] = time.Now()
	a.stateMu.Unlock()
	a.appendLog(LogEvent{Type: "cera_rejected", Market: marketNameCera, Status: marketLogStatusActive, Message: fmt.Sprintf("item_id=%d reason=%s", itemID, reason)})
}

func (a *App) ceraRejectedCount() int {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if len(a.ceraRejected) == 0 {
		return 0
	}
	now := time.Now()
	count := 0
	for itemID, reason := range a.ceraRejected {
		if reason == "" {
			continue
		}
		if t, ok := a.ceraRejectedAt[itemID]; ok && now.Sub(t) > ceraRejectedTTL {
			delete(a.ceraRejected, itemID)
			delete(a.ceraRejectedAt, itemID)
			continue
		}
		count++
	}
	return count
}

func (a *App) resetCeraRejected() {
	a.stateMu.Lock()
	a.ceraRejected = nil
	a.ceraRejectedAt = nil
	a.stateMu.Unlock()
}

func (a *App) reconcileCeraLanding(entries []ActionEntry) {
	if a.repository == nil {
		return
	}
	okIDs := map[uint32]bool{}
	for _, entry := range entries {
		if entry.Action.Market == marketNameCera && entry.Action.Operation == "" && entry.OK && entry.Action.ItemID != 0 {
			okIDs[entry.Action.ItemID] = true
		}
	}
	if len(okIDs) == 0 {
		return
	}
	time.Sleep(3 * time.Second)
	have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		a.appendLog(LogEvent{Type: "cera_landing", Market: marketNameCera, Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	missing := 0
	for itemID := range okIDs {
		if have[itemID] <= 0 {
			missing++
			a.markCeraRejected(itemID, "cera_unlanded")
		}
	}
	if missing == 0 {
		return
	}
	a.appendLog(LogEvent{Type: "cera_landing", Market: marketNameCera, Status: marketLogStatusFailed, Message: fmt.Sprintf("ok_items=%d missing=%d db_kinds=%d", len(okIDs), missing, len(have))})
	if len(have) == 0 {
		a.restartMarketService(marketServiceNamePoint, "cera ok packets did not land in database")
	}
}

func (a *App) reconcileCeraRejects(entries []ActionEntry) {
	if a.repository == nil {
		return
	}
	total := 0
	rejected118 := 0
	rejectedItems := map[uint32]bool{}
	for _, entry := range entries {
		if entry.Action.Market != marketNameCera || entry.Action.Operation != "" {
			continue
		}
		total++
		if !entry.OK && entry.Reason != nil && *entry.Reason == 118 {
			rejected118++
			if entry.Action.ItemID != 0 {
				rejectedItems[entry.Action.ItemID] = true
			}
		}
	}
	for itemID := range rejectedItems {
		a.markCeraRejected(itemID, "cera_rejected_118")
	}
	if total == 0 || rejected118 != total {
		return
	}
	have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		a.appendLog(LogEvent{Type: "cera_reject", Market: marketNameCera, Status: marketLogStatusFailed, Message: err.Error()})
		return
	}
	if len(have) != 0 {
		return
	}
	a.appendLog(LogEvent{Type: "cera_reject", Market: marketNameCera, Status: marketLogStatusFailed, Message: "all cera actions rejected reason=118 while cera db is empty"})
	a.restartMarketService(marketServiceNamePoint, "cera reason 118 with empty database")
}

//go:embed seeds/market_fallback_seed.json
var seedFiles embed.FS

type fallbackSeed struct {
	Core []corePoolItem `json:"core"`
}

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

func (a *App) reconcileRejectedStockLocked(have map[uint32]int, pvfReady bool, catalog map[uint32]catalogItem) {
	if len(a.auctionRejected) == 0 || len(have) == 0 {
		return
	}
	out := a.auctionRejected[:0]
	for _, id := range a.auctionRejected {
		if have[id] > 0 {
			a.appendAuctionAvailableLocked(id, pvfReady, catalog)
			delete(a.auctionRejectedMeta, id)
			continue
		}
		out = append(out, id)
	}
	a.auctionRejected = out
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

// ---- auction_rejection.go ----
func (a *App) markAuctionExplicitRejected(itemID uint32) {
	a.markAuctionRejected(itemID, "explicit_rejected")
}

func (a *App) applyAuctionActionFeedback(entry ActionEntry, err error) {
	if entry.Action.Market != marketNameAuction || entry.Action.Operation == "collect" || entry.Action.ItemID == 0 {
		return
	}
	if err == nil && entry.OK {
		return
	}
	a.markAuctionRejected(entry.Action.ItemID, auctionRejectionReason(entry, err))
}

func (a *App) markAuctionRejected(itemID uint32, reason string) {
	if itemID == 0 {
		return
	}
	if reason == "" {
		reason = "auction_rejected"
	}
	now := time.Now()
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.auctionQueue = removeQueueID(a.auctionQueue, itemID)
	a.auctionSpecialQueue = removeQueueID(a.auctionSpecialQueue, itemID)
	if !queueContains(a.auctionRejected, itemID) {
		a.auctionRejected = append(a.auctionRejected, itemID)
	}
	if a.auctionRejectedMeta == nil {
		a.auctionRejectedMeta = map[uint32]auctionRejectedState{}
	}
	state := a.auctionRejectedMeta[itemID]
	if state.Count == 0 {
		state.First = now
	}
	state.Last = now
	state.Count++
	state.Reason = reason
	a.auctionRejectedMeta[itemID] = state
}

func (a *App) appendAuctionAvailableLocked(itemID uint32, pvfReady bool, catalog map[uint32]catalogItem) {
	if itemID == 0 {
		return
	}
	delete(a.auctionRejectedMeta, itemID)
	if pvfReady {
		if item, ok := catalog[itemID]; ok && specialAuctionKind(item) != "" {
			if !queueContains(a.auctionSpecialQueue, itemID) {
				a.auctionSpecialQueue = append(a.auctionSpecialQueue, itemID)
			}
			return
		}
	}
	if !queueContains(a.auctionQueue, itemID) {
		a.auctionQueue = append(a.auctionQueue, itemID)
	}
}

func (a *App) pruneAuctionRejectedMetaLocked() {
	if len(a.auctionRejectedMeta) == 0 {
		return
	}
	keep := idSet(a.auctionRejected)
	for id := range a.auctionRejectedMeta {
		if !keep[id] {
			delete(a.auctionRejectedMeta, id)
		}
	}
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

// ---- auction_queue_helpers.go ----
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

// ---- auction_source.go ----
func (a *App) auctionQueueCandidates(pvfReady bool, catalog map[uint32]catalogItem) (auctionQueueCandidatesResult, error) {
	if pvfReady {
		return a.pvfItemInfoAuctionQueueCandidates(catalog)
	}
	return a.fallbackAuctionQueueCandidates()
}

func (a *App) pvfItemInfoAuctionQueueCandidates(catalog map[uint32]catalogItem) (auctionQueueCandidatesResult, error) {
	itemInfoIDs, path, err := a.currentItemInfoIDs()
	if err != nil {
		a.appendLog(LogEvent{Type: "iteminfo_gate", Status: marketLogStatusBlocked, Message: err.Error()})
		return auctionQueueCandidatesResult{Source: marketQueueSourcePVFItemInfoMissing}, nil
	}
	normal, special := catalogAuctionIDsByType(catalog, itemInfoIDs)
	a.appendLog(LogEvent{Type: "iteminfo_gate", Status: marketLogStatusActive, Message: fmt.Sprintf("source=%s allowed=%d special=%d", path, len(normal)+len(special), len(special))})
	return auctionQueueCandidatesResult{Normal: normal, Special: special, Source: marketQueueSourcePVFItemInfo}, nil
}

func (a *App) fallbackAuctionQueueCandidates() (auctionQueueCandidatesResult, error) {
	rows, err := a.fallbackAuctionRows()
	if err != nil {
		return auctionQueueCandidatesResult{}, err
	}
	ids := auctionRowIDs(rows)
	return auctionQueueCandidatesResult{Normal: ids, Source: marketQueueSourceFallback}, nil
}

func auctionRowIDs(rows []restockRow) []uint32 {
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		if row.ItemID != 0 {
			ids = append(ids, row.ItemID)
		}
	}
	return ids
}

func (a *App) auctionRowForID(pvfReady bool, catalog map[uint32]catalogItem, id uint32) (restockRow, bool) {
	if pvfReady {
		item, ok := catalog[id]
		if !ok {
			return restockRow{}, false
		}
		return a.catalogAuctionRow(item)
	}
	rows, err := a.fallbackAuctionRows()
	if err != nil {
		return restockRow{}, false
	}
	for _, row := range rows {
		if row.ItemID == id {
			return row, true
		}
	}
	return restockRow{}, false
}

func (a *App) catalogAuctionRows(catalog map[uint32]catalogItem) []restockRow {
	ids := catalogAuctionIDs(catalog, nil)
	rows := make([]restockRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := a.catalogAuctionRow(catalog[id]); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func catalogAuctionIDs(catalog map[uint32]catalogItem, allowed map[uint32]bool) []uint32 {
	normal, special := catalogAuctionIDsByType(catalog, allowed)
	return append(normal, special...)
}

func catalogAuctionIDsByType(catalog map[uint32]catalogItem, allowed map[uint32]bool) ([]uint32, []uint32) {
	ids := make([]uint32, 0, len(catalog))
	special := make([]uint32, 0)
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		if !marketCandidate(item) {
			continue
		}
		if specialAuctionKind(item) != "" {
			special = append(special, id)
		} else {
			ids = append(ids, id)
		}
	}
	sortCatalogAuctionIDs(ids, catalog)
	sortCatalogSpecialAuctionIDs(special, catalog)
	return ids, special
}

func sortCatalogAuctionIDs(ids []uint32, catalog map[uint32]catalogItem) {
	sort.Slice(ids, func(i, j int) bool {
		left := catalog[ids[i]]
		right := catalog[ids[j]]
		if left.Kind != right.Kind {
			return left.Kind == "equipment"
		}
		if left.Kind == "equipment" && left.Level != right.Level {
			return left.Level > right.Level
		}
		return left.ItemID < right.ItemID
	})
}

func sortCatalogSpecialAuctionIDs(ids []uint32, catalog map[uint32]catalogItem) {
	sort.Slice(ids, func(i, j int) bool {
		left := catalog[ids[i]]
		right := catalog[ids[j]]
		leftRank := specialAuctionRank(left)
		rightRank := specialAuctionRank(right)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Level != right.Level {
			return left.Level > right.Level
		}
		return left.ItemID < right.ItemID
	})
}

func auctionTargetRecords(row restockRow) int {
	item := row.marketItem()
	stackSize := auctionPlanStackSize(row, item, item.Kind == "equipment")
	return (row.Quantity + stackSize - 1) / stackSize
}

func (a *App) fallbackAuctionRows() ([]restockRow, error) {
	data, err := seedFiles.ReadFile("seeds/market_fallback_seed.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded market fallback: %w", err)
	}
	var seed fallbackSeed
	if err := json.Unmarshal(data, &seed); err != nil {
		return nil, fmt.Errorf("parse embedded market fallback: %w", err)
	}
	rows := make([]restockRow, 0, len(seed.Core))
	for _, item := range seed.Core {
		if item.ItemID == 0 || item.BasePrice <= 0 {
			continue
		}
		stack := a.randomStackSize(catalogItem{ItemID: item.ItemID, Kind: "stackable"})
		rows = append(rows, restockRow{
			ItemID:      item.ItemID,
			SystemPrice: item.BasePrice,
			Quantity:    stack,
			StackSize:   stack,
			Enabled:     true,
			Source:      marketRowSourceFallbackSeed,
			Kind:        "stackable",
		})
	}
	return rows, nil
}

func (a *App) catalogAuctionRow(item catalogItem) (restockRow, bool) {
	if !marketCandidate(item) {
		return restockRow{}, false
	}
	row := restockRow{
		ItemID:      item.ItemID,
		SystemPrice: marketBasePrice(item),
		Enabled:     true,
		Source:      marketRowSourcePVF,
		Kind:        item.Kind,
		Level:       item.Level,
		ItemType:    item.ItemType,
		SubType:     item.SubType,
		Slot:        item.Slot,
		Attach:      item.Attach,
		Rarity:      item.Rarity,
		StackLimit:  item.StackLimit,
	}
	if item.Kind == "equipment" {
		row.Quantity = randRange(a.rand, a.cfg.Restock.EquipmentQtyMin, a.cfg.Restock.EquipmentQtyMax)
		row.StackSize = 1
	} else {
		stack := a.randomStackSize(item)
		row.Quantity = stack
		row.StackSize = stack
	}
	return row, true
}

func (a *App) randomStackSize(item catalogItem) int {
	sizes := a.cfg.Restock.StackSizes
	if len(sizes) == 0 {
		sizes = DefaultConfig().Restock.StackSizes
	}
	stack := sizes[a.rand.Intn(len(sizes))]
	if item.StackLimit > 0 && stack > item.StackLimit {
		stack = item.StackLimit
	}
	if stack <= 0 {
		stack = 1
	}
	return stack
}

func randRange(rng *rand.Rand, min, max int) int {
	if min <= 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min + rng.Intn(max-min+1)
}

func mergeStringMap(dst *map[string]string, defaults map[string]string) {
	if *dst == nil {
		*dst = map[string]string{}
	}
	for key, value := range defaults {
		(*dst)[key] = value
	}
}

func defaultRestockComments() map[string]string {
	return map[string]string{
		"_summary":              "Normal auction uses PVF for item data and the current auction iteminfo.dat as the environment boundary. Candidate IDs are PVF auctionable IDs intersected with iteminfo.dat IDs; clicking ItemInfo explicitly releases the generated iteminfo.dat and expands that boundary.",
		"stack_sizes":           "Stackable listing count candidates, such as material bundles. The selected count is clamped by PVF stack_limit when available.",
		"equipment_qty_min":     "Minimum duplicate records generated for each missing equipment item after the DB shows no system stock for that item.",
		"equipment_qty_max":     "Maximum duplicate records generated for each missing equipment item after the DB shows no system stock for that item.",
		"equipment_inflate_min": "Lower equipment base price multiplier. PVF price/value remains the base.",
		"equipment_inflate_max": "Upper equipment base price multiplier. PVF price/value remains the base.",
		"upgrade_min":           "Minimum random equipment upgrade value written to the auction packet.",
		"upgrade_max":           "Maximum random equipment upgrade value written to the auction packet.",
		"upgrade_price_rate":    "Additional equipment price rate per upgrade level.",
		"rand_low":              "Final random price multiplier lower bound for both stackable and equipment listings.",
		"rand_high":             "Final random price multiplier upper bound for both stackable and equipment listings.",
		"max_actions":           "Maximum register packets per restock round. Default is 10000; use 0 only when a caller intentionally wants the full DB gap.",
		"max_concurrent":        "Concurrent auction register workers. This controls send pressure, not item selection.",
		"max_result_actions":    "Maximum action details retained in job result to keep UI/log payload bounded.",
		"per_item_delay_ms":     "Optional delay between actions in each worker. 0 means no intentional delay.",
	}
}
func defaultCeraComments() map[string]string {
	return map[string]string{
		"_summary":      "Gold consignment uses the fixed item list below. It is separate from normal auction item selection and does not use the PVF/iteminfo intersection gate.",
		"items":         "Gold package list. Entries with enabled=false are kept in config but not restocked.",
		"item_id":       "Gold package item ID.",
		"name":          "Display label used only for identification in config and logs.",
		"restock_price": "Consignment listing price.",
		"restock_qty":   "Target record count. Restock fills the gap when current DB stock is lower than this value.",
		"recycle_price": "Reserved reference price for future collect policy.",
		"enabled":       "Whether this gold package is enabled.",
	}
}
func defaultCeraRows() []ceraRow {
	return []ceraRow{
		{ItemID: 2675336, Label: "100w_gold", RestockPrice: 200, RestockQty: 20, RecyclePrice: 200, Enabled: true},
		{ItemID: 2675337, Label: "200w_gold", RestockPrice: 400, RestockQty: 20, RecyclePrice: 400, Enabled: true},
		{ItemID: 2675338, Label: "300w_gold", RestockPrice: 600, RestockQty: 20, RecyclePrice: 600, Enabled: true},
		{ItemID: 2675339, Label: "400w_gold", RestockPrice: 800, RestockQty: 20, RecyclePrice: 800, Enabled: true},
		{ItemID: 2675340, Label: "500w_gold", RestockPrice: 1000, RestockQty: 20, RecyclePrice: 1000, Enabled: true},
		{ItemID: 2675341, Label: "600w_gold", RestockPrice: 1200, RestockQty: 20, RecyclePrice: 1200, Enabled: true},
		{ItemID: 2675342, Label: "700w_gold", RestockPrice: 1400, RestockQty: 20, RecyclePrice: 1400, Enabled: true},
		{ItemID: 2675343, Label: "800w_gold", RestockPrice: 1600, RestockQty: 20, RecyclePrice: 1600, Enabled: true},
		{ItemID: 2675344, Label: "900w_gold", RestockPrice: 1800, RestockQty: 20, RecyclePrice: 1800, Enabled: true},
		{ItemID: 2675345, Label: "1000w_gold", RestockPrice: 2000, RestockQty: 20, RecyclePrice: 2000, Enabled: true},
		{ItemID: 2675346, Label: "2000w_gold", RestockPrice: 4000, RestockQty: 20, RecyclePrice: 4000, Enabled: true},
		{ItemID: 2675347, Label: "3000w_gold", RestockPrice: 6000, RestockQty: 20, RecyclePrice: 6000, Enabled: true},
	}
}
