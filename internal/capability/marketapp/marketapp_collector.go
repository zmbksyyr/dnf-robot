package marketapp

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type collectRow struct {
	Market       string
	AuctionID    uint64
	OwnerID      uint32
	ItemID       uint32
	Count        int32
	StartPrice   int32
	InstantPrice int32
}

func (a *App) CollectPlan(req CollectRequest) (PlanResult, error) {
	result := PlanResult{GeneratedAt: time.Now()}
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" || market == marketNameAuction {
		rows, err := a.repository.LoadCollectRows(a.cfg.AuctionDB, marketNameAuction, a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	if market == "" || market == marketNameCera || market == marketAliasGold {
		rows, err := a.repository.LoadCollectRows(a.cfg.CeraDB, marketNameCera, a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	result.Summary.Actions = len(result.Actions)
	for _, action := range result.Actions {
		switch action.Market {
		case marketNameAuction:
			result.Summary.AuctionActions++
		case marketNameCera:
			result.Summary.CeraActions++
		}
	}
	if req.MaxActions > 0 && len(result.Actions) > req.MaxActions {
		result.Actions = result.Actions[:req.MaxActions]
	}
	a.appendLog(LogEvent{Type: "collect_plan", Market: market, Summary: &result.Summary})
	return result, nil
}

func (r SQLRepository) LoadCollectRows(dbName, market string, systemOwnerBase uint32, includeSystemOwners bool) ([]collectRow, error) {
	ownerClause := "owner_id < ?"
	if includeSystemOwners {
		ownerClause = "owner_id >= 0 AND ? >= 0"
	}
	return r.loadCollectRowsWhere(dbName, market, ownerClause, systemOwnerBase)
}

func (r SQLRepository) LoadSystemCollectRows(dbName, market string, systemOwnerBase uint32) ([]collectRow, error) {
	return r.loadCollectRowsWhere(dbName, market, "owner_id >= ?", systemOwnerBase)
}

func (r SQLRepository) loadCollectRowsWhere(dbName, market, ownerClause string, systemOwnerBase uint32) ([]collectRow, error) {
	extraClause := ""
	if market == marketNameCera {
		extraClause = " AND price = -1 AND instant_price > 0"
	}
	query := fmt.Sprintf(
		"SELECT auction_id,owner_id,item_id,IFNULL(add_info,0),IFNULL(price,0),IFNULL(instant_price,0) FROM %s.`auction_main` WHERE %s%s ORDER BY auction_id ASC",
		quoteIdent(dbName), ownerClause, extraClause,
	)
	rows, err := r.db.Query(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []collectRow
	for rows.Next() {
		var row collectRow
		var count, start, instant sql.NullInt64
		row.Market = market
		if err := rows.Scan(&row.AuctionID, &row.OwnerID, &row.ItemID, &count, &start, &instant); err != nil {
			return nil, err
		}
		if count.Valid {
			row.Count = int32(count.Int64)
		}
		if start.Valid {
			row.StartPrice = int32(start.Int64)
		}
		if instant.Valid {
			row.InstantPrice = int32(instant.Int64)
		}
		if row.AuctionID == 0 {
			continue
		}
		if row.InstantPrice <= 0 {
			row.InstantPrice = row.StartPrice
		}
		if row.InstantPrice <= 0 {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r SQLRepository) CountSystemStock(dbName string, systemOwnerBase uint32) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.`auction_main` WHERE owner_id >= ?", quoteIdent(dbName))
	var count int
	if err := r.db.QueryRow(query, systemOwnerBase).Scan(&count); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (r SQLRepository) DeleteSystemStock(dbName string, systemOwnerBase uint32) (int64, error) {
	query := fmt.Sprintf("DELETE FROM %s.`auction_main` WHERE owner_id >= ?", quoteIdent(dbName))
	res, err := r.db.Exec(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return res.RowsAffected()
}

func (a *App) appendCollectActions(rows []collectRow, result *PlanResult) {
	for i, row := range rows {
		buyerID := a.cfg.SystemOwner.BuyerBase + uint32(i%maxInt(a.cfg.SystemOwner.RotateEvery, 1))
		result.Actions = append(result.Actions, Action{
			Market:       row.Market,
			Kind:         "collect",
			Operation:    "collect",
			ItemID:       row.ItemID,
			Count:        row.Count,
			UnitPrice:    row.InstantPrice,
			TotalPrice:   row.InstantPrice,
			OwnerID:      buyerID,
			OwnerName:    a.cfg.SystemOwner.OwnerName,
			CountAddInfo: row.Count,
			StartPrice:   row.StartPrice,
			InstantPrice: row.InstantPrice,
			AuctionID:    row.AuctionID,
			Source:       "auction_main",
		})
	}
}

func (a *App) appendRarityFilteredCollectActions(catalog map[uint32]catalogItem, result *PlanResult) error {
	if !a.qualityFilterEnabled() || len(catalog) == 0 || a.repository == nil {
		return nil
	}
	rows, err := a.repository.LoadSystemCollectRows(a.cfg.AuctionDB, marketNameAuction, a.cfg.SystemOwner.IDBase)
	if err != nil {
		return err
	}
	filtered := make([]collectRow, 0)
	for _, row := range rows {
		item, ok := catalog[row.ItemID]
		if !ok {
			continue
		}
		if !marketRarityAllowed(item) {
			filtered = append(filtered, row)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	a.appendCollectActions(filtered, result)
	a.appendLog(LogEvent{Type: "rarity_filter_collect", Market: marketNameAuction, Status: marketLogStatusActive, Message: fmt.Sprintf("actions=%d", len(filtered))})
	return nil
}

func (a *App) CollectOnce(req CollectRequest) (JobSummary, error) {
	if !a.jobMu.TryLock() {
		job := busyMarketJob("collect")
		return job, fmt.Errorf(job.Error)
	}
	defer a.jobMu.Unlock()
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("collect-%d", start.UnixNano()),
		Kind:      "collect",
		Status:    MarketJobStatusRunning,
		StartedAt: start,
	}
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.CollectPlan(req)
	if err != nil {
		job.Status = MarketJobStatusFailed
		job.Error = err.Error()
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error})
		return job, err
	}
	job.Plan = &plan.Summary
	maxActions := req.MaxActions
	if maxActions <= 0 {
		maxActions = a.cfg.Collector.MaxActions
	}
	actions := plan.Actions
	if maxActions > 0 && len(actions) > maxActions {
		actions = actions[:maxActions]
	}
	if !req.Execute {
		job.Status = MarketJobStatusPlanned
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
		return job, nil
	}
	failedActions, entries, firstErr := a.executeActions(job.ID, actions, req.MaxConcurrent, req.ContinueOnError, &job)
	a.reconcileCeraLanding(entries)
	if firstErr != nil && !req.ContinueOnError {
		job.Status = MarketJobStatusPartialFailed
		job.Error = firstErr.Error()
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error, Summary: job.Plan})
		return job, firstErr
	}
	if failedActions > 0 {
		job.Status = MarketJobStatusPartialFailed
		job.Error = fmt.Sprintf("%d actions failed", failedActions)
	} else {
		job.Status = MarketJobStatusSuccess
	}
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan, Message: job.Error})
	return job, firstErr
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
