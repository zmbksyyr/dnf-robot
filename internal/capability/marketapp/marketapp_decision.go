package marketapp

import (
	"fmt"
	"sort"
	"strings"
)

type marketDecisionSnapshot struct {
	Market              string
	Auction             bool
	Cera                bool
	PVFReady            bool
	PVFItems            int
	ItemInfoPath        string
	ItemInfoIDs         int
	ItemInfoError       string
	DBOwners            int
	DBAuctionKinds      int
	DBCeraKinds         int
	CeraConfigRows      int
	CeraEnabledRows     int
	CeraRejectedRows    int
	AutoMaxActions      int
	AutoMaxConcurrent   int
	EffectiveMaxActions int
	SelectedAuctionRows int
	AuctionBudget       auctionQueueBudget
	AuctionSelected     auctionQueueCounts
	QueueNormal         int
	QueueSpecial        int
	QueueRejected       int
	RejectedTracked     int
	RejectedRetryIn     int
	RejectedReasons     string
	QueueSource         string
	AuctionActions      int
	CeraActions         int
	Actions             int
	Skipped             int
	AuctionCandidates   auctionDecisionCounts
}

type auctionDecisionCounts struct {
	AllowedItemInfo int
	Intersection    int
	Normal          int
	Special         int
	Blocked         int
	Avatar          int
	Risky           int
}

func (a *App) newMarketDecisionSnapshot(market string, req RestockRequest, pvfReady bool, occ map[uint32]int, haveAuction map[uint32]int, haveCera map[uint32]int) marketDecisionSnapshot {
	return marketDecisionSnapshot{
		Market:            market,
		PVFReady:          pvfReady,
		AutoMaxActions:    req.MaxActions,
		AutoMaxConcurrent: req.MaxConcurrent,
		DBOwners:          len(occ),
		DBAuctionKinds:    len(haveAuction),
		DBCeraKinds:       len(haveCera),
		CeraConfigRows:    len(a.cfg.Cera.Items),
		CeraEnabledRows:   countEnabledCeraRows(a.cfg.Cera.Items),
		CeraRejectedRows:  a.ceraRejectedCount(),
	}
}

func (s *marketDecisionSnapshot) observeAuctionInputs(a *App, catalog map[uint32]catalogItem, pvfReady bool) {
	if !pvfReady {
		return
	}
	s.PVFItems = len(catalog)
	itemInfoIDs, path, err := a.currentItemInfoIDs()
	s.ItemInfoPath = path
	s.ItemInfoIDs = len(itemInfoIDs)
	if err != nil {
		s.ItemInfoError = err.Error()
		return
	}
	s.AuctionCandidates = a.auctionDecisionFromCatalog(catalog, itemInfoIDs)
}

func (a *App) logMarketDecision(market string, decision *marketDecisionSnapshot, summary PlanSummary) {
	if decision == nil {
		return
	}
	decision.AuctionActions = summary.AuctionActions
	decision.CeraActions = summary.CeraActions
	decision.Skipped = summary.Skipped
	decision.Actions = summary.Actions
	a.appendLog(LogEvent{Type: "market_decision", Market: market, Status: marketLogStatusActive, Message: decision.String(), Summary: &summary})
}

func (s marketDecisionSnapshot) String() string {
	fields := []marketDecisionField{
		{"auction", s.Auction},
		{"cera", s.Cera},
		{"pvf_ready", s.PVFReady},
		{"pvf_items", s.PVFItems},
		{"iteminfo_ids", s.ItemInfoIDs},
		{"iteminfo_allowed", s.AuctionCandidates.AllowedItemInfo},
		{"iteminfo_path", s.ItemInfoPath},
		{"iteminfo_error", fmt.Sprintf("%q", s.ItemInfoError)},
		{"intersection", s.AuctionCandidates.Intersection},
		{"normal", s.AuctionCandidates.Normal},
		{"special", s.AuctionCandidates.Special},
		{"filtered_blocked", s.AuctionCandidates.Blocked},
		{"filtered_avatar", s.AuctionCandidates.Avatar},
		{"filtered_risky", s.AuctionCandidates.Risky},
		{"db_owners", s.DBOwners},
		{"db_auction_kinds", s.DBAuctionKinds},
		{"db_cera_kinds", s.DBCeraKinds},
		{"cera_config", s.CeraConfigRows},
		{"cera_enabled", s.CeraEnabledRows},
		{"cera_rejected", s.CeraRejectedRows},
		{"queue_normal", s.QueueNormal},
		{"queue_special", s.QueueSpecial},
		{"queue_rejected", s.QueueRejected},
		{"rejected_tracked", s.RejectedTracked},
		{"rejected_retry_in", s.RejectedRetryIn},
		{"rejected_reasons", s.RejectedReasons},
		{"queue_source", s.QueueSource},
		{"budget_normal", s.AuctionBudget.Normal},
		{"budget_special", s.AuctionBudget.Special},
		{"budget_rejected", s.AuctionBudget.Rejected},
		{"selected_normal", s.AuctionSelected.Normal},
		{"selected_special", s.AuctionSelected.Special},
		{"selected_rejected", s.AuctionSelected.Rejected},
		{"selected_auction_rows", s.SelectedAuctionRows},
		{"actions", s.Actions},
		{"auction_actions", s.AuctionActions},
		{"cera_actions", s.CeraActions},
		{"skipped", s.Skipped},
		{"max_actions", s.AutoMaxActions},
		{"effective_max_actions", s.EffectiveMaxActions},
		{"max_concurrent", s.AutoMaxConcurrent},
	}
	return joinMarketDecisionFields(fields)
}

type marketDecisionField struct {
	key   string
	value interface{}
}

func joinMarketDecisionFields(fields []marketDecisionField) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", field.key, field.value))
	}
	return strings.Join(parts, " ")
}

func (s *marketDecisionSnapshot) captureQueues(a *App) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	s.applyQueueSnapshot(a.auctionQueueSnapshotLocked())
}

func (s *marketDecisionSnapshot) applyQueueSnapshot(snapshot auctionQueueSnapshot) {
	s.QueueNormal = snapshot.Normal
	s.QueueSpecial = snapshot.Special
	s.QueueRejected = snapshot.Rejected
	s.RejectedTracked = snapshot.RejectedTracked
	s.RejectedRetryIn = snapshot.RejectedRetryIn
	s.RejectedReasons = snapshot.RejectedReasons
	s.QueueSource = snapshot.Source
}

func topAuctionRejectedReasons(meta map[uint32]auctionRejectedState, limit int) string {
	if len(meta) == 0 || limit <= 0 {
		return ""
	}
	counts := map[string]int{}
	for _, state := range meta {
		reason := state.Reason
		if reason == "" {
			reason = "unknown"
		}
		counts[reason] += state.Count
	}
	type reasonCount struct {
		reason string
		count  int
	}
	items := make([]reasonCount, 0, len(counts))
	for reason, count := range counts {
		items = append(items, reasonCount{reason: reason, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].reason < items[j].reason
		}
		return items[i].count > items[j].count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s:%d", item.reason, item.count))
	}
	return strings.Join(parts, ",")
}

func (a *App) auctionDecisionFromCatalog(catalog map[uint32]catalogItem, allowed map[uint32]bool) auctionDecisionCounts {
	counts := auctionDecisionCounts{AllowedItemInfo: len(allowed)}
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		counts.Intersection++
		switch {
		case item.ItemID == 0 || item.Kind == "blocked":
			counts.Blocked++
		case !marketRarityAllowed(item) && a.qualityFilterEnabled():
			counts.Blocked++
		case isAvatarEquipment(item):
			counts.Avatar++
		case specialAuctionKind(item) != "":
			counts.Special++
		case isRiskyPVFItem(item):
			counts.Risky++
		default:
			counts.Normal++
		}
	}
	return counts
}

func countEnabledCeraRows(rows []ceraRow) int {
	count := 0
	for _, row := range rows {
		if row.Enabled && row.ItemID != 0 && row.RestockQty > 0 {
			count++
		}
	}
	return count
}
