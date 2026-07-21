package marketapp

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed seeds/market_fallback_seed.json
var seedFiles embed.FS

type fallbackSeed struct {
	Core []corePoolItem `json:"core"`
}

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
	normal, special := a.catalogAuctionIDsByType(catalog, itemInfoIDs)
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
	return (*App)(nil).catalogAuctionIDsByType(catalog, allowed)
}

func (a *App) catalogAuctionIDsByType(catalog map[uint32]catalogItem, allowed map[uint32]bool) ([]uint32, []uint32) {
	ids := make([]uint32, 0, len(catalog))
	special := make([]uint32, 0)
	for id, item := range catalog {
		if allowed != nil && !allowed[id] {
			continue
		}
		if !a.marketCandidate(item) {
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
	if !a.marketCandidate(item) {
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
