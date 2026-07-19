package marketapp

import (
	"fmt"
	"time"
)

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
