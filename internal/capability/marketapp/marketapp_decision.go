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
	s.AuctionCandidates = auctionDecisionFromCatalog(catalog, itemInfoIDs)
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
	return fmt.Sprintf(
		"auction=%t cera=%t pvf_ready=%t pvf_items=%d iteminfo_ids=%d iteminfo_allowed=%d iteminfo_path=%s iteminfo_error=%q intersection=%d normal=%d special=%d filtered_blocked=%d filtered_avatar=%d filtered_risky=%d db_owners=%d db_auction_kinds=%d db_cera_kinds=%d cera_config=%d cera_enabled=%d cera_rejected=%d queue_normal=%d queue_special=%d queue_rejected=%d rejected_tracked=%d rejected_retry_in=%d rejected_reasons=%s queue_source=%s budget_normal=%d budget_special=%d budget_rejected=%d selected_auction_rows=%d actions=%d auction_actions=%d cera_actions=%d skipped=%d max_actions=%d effective_max_actions=%d max_concurrent=%d",
		s.Auction,
		s.Cera,
		s.PVFReady,
		s.PVFItems,
		s.ItemInfoIDs,
		s.AuctionCandidates.AllowedItemInfo,
		s.ItemInfoPath,
		s.ItemInfoError,
		s.AuctionCandidates.Intersection,
		s.AuctionCandidates.Normal,
		s.AuctionCandidates.Special,
		s.AuctionCandidates.Blocked,
		s.AuctionCandidates.Avatar,
		s.AuctionCandidates.Risky,
		s.DBOwners,
		s.DBAuctionKinds,
		s.DBCeraKinds,
		s.CeraConfigRows,
		s.CeraEnabledRows,
		s.CeraRejectedRows,
		s.QueueNormal,
		s.QueueSpecial,
		s.QueueRejected,
		s.RejectedTracked,
		s.RejectedRetryIn,
		s.RejectedReasons,
		s.QueueSource,
		s.AuctionBudget.Normal,
		s.AuctionBudget.Special,
		s.AuctionBudget.Rejected,
		s.SelectedAuctionRows,
		s.Actions,
		s.AuctionActions,
		s.CeraActions,
		s.Skipped,
		s.AutoMaxActions,
		s.EffectiveMaxActions,
		s.AutoMaxConcurrent,
	)
}

func (s *marketDecisionSnapshot) captureQueues(a *App) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	s.QueueNormal = len(a.auctionQueue)
	s.QueueSpecial = len(a.auctionSpecialQueue)
	s.QueueRejected = len(a.auctionRejected)
	s.RejectedTracked = len(a.auctionRejectedMeta)
	s.RejectedRetryIn = auctionRejectedRetryEvery - a.auctionRejectedTick
	if s.QueueRejected == 0 {
		s.RejectedRetryIn = 0
	}
	s.RejectedReasons = topAuctionRejectedReasons(a.auctionRejectedMeta, 5)
	s.QueueSource = a.auctionQueueSource
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

func auctionDecisionFromCatalog(catalog map[uint32]catalogItem, allowed map[uint32]bool) auctionDecisionCounts {
	counts := auctionDecisionCounts{AllowedItemInfo: len(allowed)}
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		counts.Intersection++
		switch {
		case item.ItemID == 0 || item.Kind == "blocked":
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
