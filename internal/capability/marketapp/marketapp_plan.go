package marketapp

import (
	"strings"
	"time"
)

func (a *App) Plan(req RestockRequest) (PlanResult, error) {
	market, needAuction, needCera := requestedRestockMarkets(req.Market)
	catalog, pvfReady := a.loadAuctionCatalog(needAuction)
	occ, haveAuction, haveCera, err := a.loadSystemStock()
	if err != nil {
		return PlanResult{}, err
	}

	result := PlanResult{GeneratedAt: time.Now()}
	result.Summary.ExistingRecords = len(occ)
	decision := a.newMarketDecisionSnapshot(market, req, pvfReady, occ, haveAuction, haveCera)

	if needAuction {
		if err := a.planAuctionMarket(req, catalog, pvfReady, haveAuction, occ, &decision, &result); err != nil {
			return PlanResult{}, err
		}
	}
	if needCera {
		a.planCeraMarket(haveCera, occ, &decision, &result)
	}

	result.Actions = limitActions(result.Actions, req.MaxActions)
	result.Summary = summarizePlan(result.Actions, result.Skipped, result.Summary.ExistingRecords)
	a.logMarketDecision(market, &decision, result.Summary)
	return result, nil
}

// Restock planning is kept as function islands: market routing, auction PVF/iteminfo
// boundary, cera fixed-list planning, and final summary/decision logging.
func requestedRestockMarkets(market string) (string, bool, bool) {
	normalized := strings.ToLower(strings.TrimSpace(market))
	return normalized, normalized == "" || normalized == marketNameAuction, normalized == "" || normalized == marketNameCera || normalized == marketAliasGold
}

func (a *App) loadAuctionCatalog(needAuction bool) (map[uint32]catalogItem, bool) {
	if !needAuction {
		return nil, false
	}
	catalog, err := a.loadCatalog()
	if err != nil {
		a.appendLog(LogEvent{Type: "pvf_catalog", Status: marketLogStatusFallback, Message: err.Error()})
		return nil, false
	}
	return catalog, true
}

func (a *App) planAuctionMarket(req RestockRequest, catalog map[uint32]catalogItem, pvfReady bool, haveAuction map[uint32]int, occ map[uint32]int, decision *marketDecisionSnapshot, result *PlanResult) error {
	decision.Auction = true
	decision.observeAuctionInputs(a, catalog, pvfReady)
	maxActions := req.MaxActions
	if maxActions <= 0 {
		maxActions = a.cfg.Restock.MaxActions
	}
	decision.EffectiveMaxActions = maxActions
	var rows []restockRow
	if len(req.ItemIDs) > 0 {
		selection, err := a.targetAuctionSelection(pvfReady, catalog, haveAuction, req.ItemIDs)
		if err != nil {
			return err
		}
		rows = selection.Rows
		decision.AuctionSelected = selection.Selected
	} else {
		selection, err := a.nextAuctionQueueSelection(pvfReady, catalog, haveAuction, maxActions)
		if err != nil {
			return err
		}
		rows = selection.Rows
		decision.AuctionBudget = selection.Budget
		decision.AuctionSelected = selection.Selected
	}
	decision.SelectedAuctionRows = len(rows)
	a.planAuction(rows, catalog, haveAuction, occ, result)
	decision.captureQueues(a)
	return nil
}

func (a *App) planCeraMarket(haveCera map[uint32]int, occ map[uint32]int, decision *marketDecisionSnapshot, result *PlanResult) {
	decision.Cera = true
	a.planCera(a.cfg.Cera.Items, nil, haveCera, occ, result)
}

func summarizePlan(actions []Action, skipped []SkippedItem, existingRecords int) PlanSummary {
	summary := PlanSummary{Actions: len(actions), Skipped: len(skipped), ExistingRecords: existingRecords}
	for _, action := range actions {
		if action.Market == marketNameAuction {
			summary.AuctionActions++
		}
		if action.Market == marketNameCera {
			summary.CeraActions++
		}
		switch action.Kind {
		case "title", "creature", "artifact red", "artifact blue", "artifact green":
			summary.Special++
		}
	}
	for _, skipped := range skipped {
		switch skipped.Reason {
		case "missing_from_pvf":
			summary.Missing++
		case "risky_special_type":
			summary.Risky++
		case "not_auctionable", "avatar_not_auctionable", "requires_add_info":
			summary.NotAuctionable++
		}
	}
	return summary
}

func limitActions(actions []Action, maxActions int) []Action {
	if maxActions > 0 && len(actions) > maxActions {
		return actions[:maxActions]
	}
	return actions
}
