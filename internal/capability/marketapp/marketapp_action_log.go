package marketapp

import (
	"fmt"
	"sort"
)

type actionLogAccumulator struct {
	total       int
	ok          int
	failed      int
	errorCount  int
	byMarket    map[string]int
	byReason    map[string]int
	failedItems map[uint32]actionLogFailedItem
}

type actionLogFailedItem struct {
	count  int
	reason string
}

func newActionLogAccumulator() actionLogAccumulator {
	return actionLogAccumulator{
		byMarket:    map[string]int{},
		byReason:    map[string]int{},
		failedItems: map[uint32]actionLogFailedItem{},
	}
}

func (s *actionLogAccumulator) add(entry ActionEntry, err error) {
	s.total++
	s.byMarket[entry.Action.Market]++
	if err == nil && entry.OK {
		s.ok++
		return
	}
	s.failed++
	reason := actionLogReason(entry, err)
	s.byReason[reason]++
	if err != nil {
		s.errorCount++
	}
	if entry.Action.ItemID != 0 {
		item := s.failedItems[entry.Action.ItemID]
		item.count++
		if item.reason == "" {
			item.reason = reason
		}
		s.failedItems[entry.Action.ItemID] = item
	}
}

func (s actionLogAccumulator) summary() ActionLogSummary {
	return ActionLogSummary{
		Total:      s.total,
		OK:         s.ok,
		Failed:     s.failed,
		ErrorCount: s.errorCount,
		ByMarket:   compactCountMap(s.byMarket),
		ByReason:   compactCountMap(s.byReason),
		TopFailed:  topActionLogFailedItems(s.failedItems, 20),
	}
}

func actionLogReason(entry ActionEntry, err error) string {
	if err != nil {
		return "executor_error"
	}
	if entry.Reason != nil {
		return fmt.Sprintf("%d", *entry.Reason)
	}
	if actionRequiresAuctionID(entry.Action) && entry.AuctionID == 0 {
		return "missing_auction_id"
	}
	return "rejected"
}

func auctionRejectionReason(entry ActionEntry, err error) string {
	return actionLogReason(entry, err)
}

func actionRequiresAuctionID(action Action) bool {
	return action.Market == marketNameAuction && action.Operation != "collect"
}

func compactCountMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		if k != "" && v > 0 {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func topActionLogFailedItems(in map[uint32]actionLogFailedItem, limit int) []ActionLogItem {
	if len(in) == 0 || limit <= 0 {
		return nil
	}
	items := make([]ActionLogItem, 0, len(in))
	for id, stat := range in {
		items = append(items, ActionLogItem{ItemID: id, Count: stat.count, Reason: stat.reason})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].ItemID < items[j].ItemID
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
