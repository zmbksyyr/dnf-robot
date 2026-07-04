package marketapp

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"robot/internal/foundation/lockhub"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- auction_guard.go ----
const defaultDFGameRJSPath = "/dp2/df_game_r.js"

const auctionSearchGuardBegin = "// DP2_AUCTION_SEARCH_HOOK_GUARD_BEGIN"
const auctionSearchGuardEnd = "// DP2_AUCTION_SEARCH_HOOK_GUARD_END"

const auctionSearchGuardSource = auctionSearchGuardBegin + `
(function () {
    var root = (typeof globalThis !== 'undefined') ? globalThis : this;
    var key = '__dp2_auction_search_hook_guard_v1__';
    if (root[key]) {
        return;
    }
    root[key] = true;

    var blocked = {};
    blocked[ptr('0x084D75BC').toString().toLowerCase()] = true;

    var rawReplace = Interceptor.replace.bind(Interceptor);
    var rawRevert = Interceptor.revert.bind(Interceptor);

    function addrOf(target) {
        try {
            return ptr(target).toString().toLowerCase();
        } catch (e) {
            try {
                return target.toString().toLowerCase();
            } catch (_) {
                return '';
            }
        }
    }

    Interceptor.replace = function (target, replacement) {
        var addr = addrOf(target);
        if (blocked[addr]) {
            try {
                rawRevert(target);
                Interceptor.flush();
            } catch (e) {
            }
            console.log('[dp2 guard] blocked auction search Interceptor.replace at ' + addr);
            return;
        }
        return rawReplace(target, replacement);
    };

    console.log('[dp2 guard] auction search hook guard installed');
})();
` + auctionSearchGuardEnd + `

`

func (a *App) InstallAuctionSearchGuard(req AuctionSearchGuardRequest) (AuctionSearchGuardResult, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = defaultDFGameRJSPath
	}
	result := AuctionSearchGuardResult{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Contains(data, []byte(auctionSearchGuardBegin)) {
		result.Installed = true
		result.Message = "auction search hook guard already installed"
		a.appendLog(LogEvent{Type: "auction_guard", Status: "exists", Message: path})
		return result, nil
	}
	backup := fmt.Sprintf("%s.bak_auction_guard_%s", path, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return result, fmt.Errorf("prepare backup dir: %w", err)
	}
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return result, fmt.Errorf("backup %s: %w", backup, err)
	}
	next := append([]byte(auctionSearchGuardSource), data...)
	if err := os.WriteFile(path, next, 0644); err != nil {
		return result, fmt.Errorf("write %s: %w", path, err)
	}
	result.Backup = backup
	result.Installed = true
	result.Changed = true
	result.Message = "auction search hook guard installed; restart df_game_r to apply"
	a.appendLog(LogEvent{Type: "auction_guard", Status: "installed", Message: fmt.Sprintf("%s backup=%s", path, backup)})
	return result, nil
}

// ---- auto.go ----
func (a *App) StartAuto() {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoRun {
		return
	}
	a.stopAuto = make(chan struct{})
	a.autoDone = make(chan struct{})
	a.autoRun = true
	go a.autoLoop()
}

func (a *App) StopAuto() {
	a.autoMu.Lock()
	if !a.autoRun {
		a.autoMu.Unlock()
		return
	}
	stop := a.stopAuto
	done := a.autoDone
	close(stop)
	a.autoMu.Unlock()
	<-done
}

func (a *App) Shutdown() {
	a.StopAuto()
}

func (a *App) markAutoStopped() {
	a.autoMu.Lock()
	a.autoRun = false
	a.autoMu.Unlock()
}

func (a *App) AutoRunning() bool {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return a.autoRun
}

func (a *App) autoLoop() {
	defer func() {
		a.markAutoStopped()
		close(a.autoDone)
	}()
	a.mu.Lock()
	enabled := a.cfg.Auto.Enabled
	initialMS := a.cfg.Auto.InitialDelayMS
	intervalMS := a.cfg.Auto.IntervalMS
	a.mu.Unlock()
	if !enabled {
		a.appendLog(LogEvent{Type: "auto", Status: "disabled"})
		return
	}
	initial := time.Duration(initialMS) * time.Millisecond
	if initial > 0 {
		select {
		case <-time.After(initial):
		case <-a.stopAuto:
			return
		}
	}
	a.runAutoOnce()
	interval := time.Duration(intervalMS) * time.Millisecond
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.runAutoOnce()
		case <-a.stopAuto:
			return
		}
	}
}

func (a *App) runAutoOnce() {
	if tables, err := a.repository.EnsureMarketTables(a.marketDBNames(), time.Now()); err != nil {
		a.mu.Lock()
		a.dbInit = tables
		a.dbInitErr = err.Error()
		a.mu.Unlock()
		a.appendLog(LogEvent{Type: "db_init", Status: "failed", Message: err.Error()})
	} else {
		a.mu.Lock()
		a.dbInit = tables
		a.dbInitErr = ""
		a.mu.Unlock()
	}
	markets := a.cfg.Auto.Markets
	if len(markets) == 0 {
		markets = []string{"auction", "cera"}
	}
	if !a.dfGameRRunning() {
		a.appendLog(LogEvent{Type: "auto", Status: "game_down", Message: "df_game_r is not running; market services skipped"})
		return
	}
	a.ensureMarketServices(markets)
	for _, market := range markets {
		market = strings.ToLower(strings.TrimSpace(market))
		if market == "" {
			continue
		}
		if a.cfg.Collector.Enabled {
			a.appendLog(LogEvent{Type: "auto_collect", Market: market, Status: "start"})
			job, err := a.CollectOnce(CollectRequest{
				Market:          market,
				Execute:         true,
				MaxActions:      a.cfg.Auto.MaxActions,
				MaxConcurrent:   a.cfg.Auto.MaxConcurrent,
				ContinueOnError: a.cfg.Auto.ContinueOnError,
			})
			status := job.Status
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			a.appendLog(LogEvent{Type: "auto_collect", JobID: job.ID, Market: market, Status: status, Message: msg})
		}
		a.appendLog(LogEvent{Type: "auto_run", Market: market, Status: "start"})
		job, err := a.RestockOnce(RestockRequest{
			Market:          market,
			Execute:         true,
			MaxActions:      a.cfg.Auto.MaxActions,
			MaxConcurrent:   a.cfg.Auto.MaxConcurrent,
			ContinueOnError: a.cfg.Auto.ContinueOnError,
		})
		status := job.Status
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		a.appendLog(LogEvent{Type: "auto_run", JobID: job.ID, Market: market, Status: status, Message: msg})
	}
}

func (a *App) dfGameRRunning() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	name := filepath.Base(strings.TrimSpace(a.dfGameR))
	if name == "." || name == "/" || name == "" {
		name = "df_game_r"
	}
	out, err := exec.Command("pidof", name).Output()
	if err == nil && len(strings.Fields(string(out))) > 0 {
		return true
	}
	out, err = exec.Command("pgrep", "-f", "(^|/)"+regexp.QuoteMeta(name)+"( |$)").Output()
	return err == nil && len(strings.Fields(string(out))) > 0
}

func (a *App) ensureMarketServices(markets []string) {
	needAuction := false
	needPoint := false
	for _, market := range markets {
		switch strings.ToLower(strings.TrimSpace(market)) {
		case "", "auction":
			needAuction = true
		case "cera", "gold", "point":
			needPoint = true
		}
	}
	if len(markets) == 0 {
		needAuction = true
		needPoint = true
	}
	if needAuction {
		a.ensureMarketService("auction", "127.0.0.1:30803", "/home/neople/auction", "./df_auction_r", []string{"./cfg/auction_cain.cfg", "start", "./df_auction_r"})
	}
	if needPoint {
		a.ensureMarketService("point", "127.0.0.1:30603", "/home/neople/point", "./df_point_r", []string{"./cfg/point_cain.cfg", "start", "df_point_r"})
	}
}

func (a *App) ensureMarketService(name, addr, dir, bin string, args []string) {
	if tcpReady(addr, 500*time.Millisecond) {
		if name == "auction" {
			a.patchAuctionMemoryIfRunning("service_ready")
		}
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: "start_failed", Message: err.Error()})
		return
	}
	a.appendLog(LogEvent{Type: "market_service", Market: name, Status: "start", Message: fmt.Sprintf("pid=%d addr=%s", cmd.Process.Pid, addr)})
	go func() {
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if tcpReady(addr, 500*time.Millisecond) {
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: "ready", Message: addr})
			if name == "auction" {
				a.patchAuctionMemoryIfRunning("service_started")
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	a.appendLog(LogEvent{Type: "market_service", Market: name, Status: "not_ready", Message: addr})
}

func tcpReady(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ---- collector.go ----
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
	if market == "" || market == "auction" {
		rows, err := a.repository.LoadCollectRows(a.cfg.AuctionDB, "auction", a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	if market == "" || market == "cera" || market == "gold" {
		rows, err := a.repository.LoadCollectRows(a.cfg.CeraDB, "cera", a.cfg.SystemOwner.IDBase, a.cfg.Collector.IncludeSystemOwners)
		if err != nil {
			return PlanResult{}, err
		}
		a.appendCollectActions(rows, &result)
	}
	result.Summary.Actions = len(result.Actions)
	for _, action := range result.Actions {
		switch action.Market {
		case "auction":
			result.Summary.AuctionActions++
		case "cera":
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
	extraClause := ""
	if market == "cera" {
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
		if row.AuctionID == 0 || row.OwnerID >= systemOwnerBase && !includeSystemOwners {
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

func (a *App) CollectOnce(req CollectRequest) (JobSummary, error) {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("collect-%d", start.UnixNano()),
		Kind:      "collect",
		Status:    "running",
		StartedAt: start,
	}
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.CollectPlan(req)
	if err != nil {
		job.Status = "failed"
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
		job.Status = "planned"
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
		return job, nil
	}
	failedActions, _, firstErr := a.executeActions(job.ID, actions, req.MaxConcurrent, req.ContinueOnError, &job)
	if firstErr != nil && !req.ContinueOnError {
		job.Status = "partial_failed"
		job.Error = firstErr.Error()
		job.EndedAt = time.Now()
		job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
		a.setLastJob(job)
		a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Message: job.Error, Summary: job.Plan})
		return job, firstErr
	}
	if failedActions > 0 {
		job.Status = "partial_failed"
		job.Error = fmt.Sprintf("%d actions failed", failedActions)
	} else {
		job.Status = "success"
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

// ---- db.go ----
var mysqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (a *App) marketDBNames() []string {
	return []string{a.cfg.AuctionDB, a.cfg.CeraDB}
}

func (r SQLRepository) EnsureMarketTables(dbNames []string, now time.Time) ([]string, error) {
	seen := map[string]bool{}
	var ensured []string
	for _, dbName := range dbNames {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" || seen[dbName] {
			continue
		}
		seen[dbName] = true
		tables, err := r.ensureAuctionMonthlyTables(dbName, now)
		ensured = append(ensured, tables...)
		if err != nil {
			return ensured, err
		}
	}
	return ensured, nil
}

func (r SQLRepository) ensureAuctionMonthlyTables(dbName string, now time.Time) ([]string, error) {
	if !mysqlIdentifierPattern.MatchString(dbName) {
		return nil, fmt.Errorf("invalid auction db %q", dbName)
	}
	yyyymm := now.Format("200601")
	targets := []struct {
		base string
		name string
	}{
		{base: "auction_history", name: "auction_history_" + yyyymm},
		{base: "auction_history_buyer", name: "auction_history_buyer_" + yyyymm},
	}
	created := make([]string, 0, len(targets))
	for _, target := range targets {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` LIKE `%s`.`%s`", dbName, target.name, dbName, target.base)
		if _, err := r.db.Exec(query); err != nil {
			return created, fmt.Errorf("ensure monthly table %s.%s: %w", dbName, target.name, err)
		}
		created = append(created, dbName+"."+target.name)
	}
	return created, nil
}

func (a *App) loadSystemStock() (map[uint32]int, map[uint32]int, map[uint32]int, error) {
	occ := map[uint32]int{}
	auctionHave, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	ceraHave, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	return occ, auctionHave, ceraHave, nil
}

func (r SQLRepository) LoadMarketStock(dbName string, systemOwnerBase uint32, occ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	query := "SELECT owner_id,item_id,COUNT(*) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE owner_id >= ? GROUP BY owner_id,item_id"
	rows, err := r.db.Query(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return out, nil
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID, itemID uint32
		var count int
		if err := rows.Scan(&ownerID, &itemID, &count); err != nil {
			return nil, err
		}
		occ[ownerID] += count
		out[itemID] += count
	}
	return out, rows.Err()
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "unknown database") || strings.Contains(msg, "no such table")
}

func quoteIdent(v string) string {
	parts := strings.Split(v, ".")
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}

// ---- planner.go ----
func (a *App) planAuction(rows []restockRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.Quantity <= 0 || !row.Enabled {
			continue
		}
		if row.Kind == "" {
			if catalogItem, ok := catalog[row.ItemID]; ok {
				row.applyMarketItem(catalogItem)
			}
		}
		item := row.marketItem()
		if item.Name == "" {
			item.Name = row.Name
		}
		if item.Kind == "blocked" {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: item.Name, Reason: "not_auctionable"})
			continue
		}
		if isRiskyPVFItem(item) {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: item.Name, Reason: "risky_special_type"})
			continue
		}
		isEquip := item.Kind == "equipment"
		if row.SealFlag != 0 && !isEquip {
			result.Skipped = append(result.Skipped, SkippedItem{Market: "auction", ItemID: row.ItemID, Name: row.Name, Reason: "requires_add_info"})
			continue
		}
		stackSize := row.StackSize
		if stackSize <= 0 {
			stackSize = 1
		}
		if isEquip {
			stackSize = 1
		}
		if !isEquip && item.StackLimit > 0 && stackSize > item.StackLimit {
			stackSize = item.StackLimit
		}
		targetRecords := (row.Quantity + stackSize - 1) / stackSize
		current := have[row.ItemID]
		if current > 0 {
			continue
		}
		batchInflate := 1.0
		if isEquip {
			batchInflate = float64(randRange(a.rand, a.cfg.Restock.EquipInflateMin, a.cfg.Restock.EquipInflateMax))
		}
		for i := 0; i < targetRecords; i++ {
			pos := i
			count := int32(1)
			if !isEquip {
				if pos < targetRecords-1 {
					count = int32(stackSize)
				} else {
					count = int32(row.Quantity - (targetRecords-1)*stackSize)
				}
			}
			addInfo := int32(0)
			upgrade := 0
			extraAddInfo := int32(0)
			if isEquip {
				addInfo = 0
				upgrade = row.Upgrade
				if upgrade <= 0 {
					upgrade = randRange(a.rand, a.cfg.Restock.UpgradeMin, a.cfg.Restock.UpgradeMax)
				}
				extraAddInfo = int32(randRange(a.rand, a.cfg.Restock.RefineMin, a.cfg.Restock.RefineMax))
			} else {
				addInfo = count
			}
			unit := a.auctionUnitPrice(row.SystemPrice, isEquip, batchInflate, upgrade, int(extraAddInfo))
			total := unit
			if !isEquip {
				total = unit * count
			}
			ownerID := a.pickOwner(occ)
			source := row.Source
			if source == "" {
				source = "legacy_seed"
			}
			result.Actions = append(result.Actions, Action{
				Market:       "auction",
				Kind:         item.Kind,
				ItemID:       row.ItemID,
				Name:         item.Name,
				Count:        count,
				UnitPrice:    unit,
				TotalPrice:   total,
				OwnerID:      ownerID,
				OwnerName:    a.cfg.SystemOwner.OwnerName,
				CountAddInfo: addInfo,
				StartPrice:   total - 1,
				InstantPrice: total,
				Upgrade:      upgrade,
				ExtraAddInfo: extraAddInfo,
				Source:       source,
			})
		}
		have[row.ItemID] = targetRecords
	}
}

func (a *App) planCera(rows []ceraRow, catalog map[uint32]catalogItem, have map[uint32]int, occ map[uint32]int, result *PlanResult) {
	for _, row := range rows {
		if row.ItemID == 0 || row.RestockQty <= 0 || !row.Enabled {
			continue
		}
		if catalog != nil {
			if _, ok := catalog[row.ItemID]; !ok {
				result.Skipped = append(result.Skipped, SkippedItem{Market: "cera", ItemID: row.ItemID, Name: row.Label, Reason: "missing_from_pvf"})
				continue
			}
		}
		current := have[row.ItemID]
		need := row.RestockQty - current
		for i := 0; i < need; i++ {
			ownerID := a.pickOwner(occ)
			price := a.price(row.RestockPrice)
			result.Actions = append(result.Actions, Action{
				Market:       "cera",
				Kind:         "gold",
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
				Source:       "cera_config",
			})
		}
	}
}

func (a *App) price(base int32) int32 {
	if base <= 0 {
		base = 1
	}
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low <= 0 || high <= 0 || low == high {
		return base
	}
	v := float64(base) * (low + a.rand.Float64()*(high-low))
	if v < 1 {
		return 1
	}
	return int32(v)
}

func (a *App) auctionUnitPrice(base int32, isEquipment bool, batchInflate float64, upgrade, refine int) int32 {
	if !isEquipment {
		return a.price(base)
	}
	if base <= 0 {
		base = 1000
	}
	if batchInflate <= 0 {
		batchInflate = 1
	}
	price := float64(base) * batchInflate
	price *= 1 + float64(upgrade)*a.cfg.Restock.UpgradePriceRate
	price *= 1 + float64(refine)*a.cfg.Restock.RefinePriceRate
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low > 0 && high > 0 && low != high {
		if high < low {
			high = low
		}
		price *= low + a.rand.Float64()*(high-low)
	}
	if price < 1 {
		return 1
	}
	const maxAuctionPrice = int32(2_000_000_000)
	if price > float64(maxAuctionPrice) {
		return maxAuctionPrice
	}
	return int32(price)
}

func marketBasePrice(item catalogItem) int32 {
	base := item.Price
	if base <= 0 {
		base = item.Value
	}
	if base <= 0 {
		base = 1000
	}
	return base
}

func (a *App) pickOwner(occ map[uint32]int) uint32 {
	owner := a.cfg.SystemOwner.IDBase
	for occ[owner] >= a.cfg.SystemOwner.RotateEvery {
		owner++
	}
	occ[owner]++
	return owner
}

func isRiskyPVFItem(item catalogItem) bool {
	if isKnownZeroSuccessEquipment(item) {
		return true
	}
	switch item.ItemType {
	case 2, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30:
		return true
	default:
		return false
	}
}

func isKnownZeroSuccessEquipment(item catalogItem) bool {
	if item.Kind != "equipment" {
		return false
	}
	attach := strings.ToLower(strings.TrimSpace(item.Attach))
	slot := strings.ToLower(strings.TrimSpace(item.Slot))
	if attach == "" {
		return true
	}
	if attach != "free" {
		return false
	}
	switch slot {
	case "coatavatar", "hairavatar", "pantsavatar", "hatavatar", "faceavatar", "breastavatar", "shoesavatar", "creature":
		return true
	default:
		return false
	}
}

// ---- source.go ----
//
//go:embed seeds/market_fallback_seed.json
var seedFiles embed.FS

type fallbackSeed struct {
	Core []corePoolItem `json:"core"`
}

func cleanupLegacyMarketFiles(configDir string) {
	_ = os.Remove(filepath.Join(configDir, "market_pool.json"))
	_ = os.Remove(filepath.Join(configDir, "market_restock.json"))
	_ = os.Remove(filepath.Join(configDir, "market_probe_pool.json"))
}

func (a *App) auctionQueueIsPVF() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.auctionQueueSource == "pvf" && len(a.auctionQueue) > 0
}

func (a *App) nextAuctionQueueRows(pvfReady bool, catalog map[uint32]catalogItem, have map[uint32]int, maxActions int) ([]restockRow, error) {
	a.mu.Lock()
	needsLoad := len(a.auctionQueue) == 0 || pvfReady && a.auctionQueueSource != "pvf"
	a.mu.Unlock()
	if needsLoad {
		candidates, source, err := a.auctionQueueCandidates(pvfReady, catalog)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		if len(a.auctionQueue) == 0 || source == "pvf" && a.auctionQueueSource != "pvf" {
			a.auctionQueue = append([]uint32(nil), candidates...)
			a.auctionQueueSource = source
		}
		a.mu.Unlock()
	}

	a.mu.Lock()
	queueLen := len(a.auctionQueue)
	selected := make([]restockRow, 0)
	planned := 0
	for i := 0; i < queueLen; i++ {
		id := a.auctionQueue[0]
		a.auctionQueue = append(a.auctionQueue[1:], id)
		if have[id] > 0 {
			continue
		}
		row, ok := a.auctionRowForID(pvfReady, catalog, id)
		if !ok {
			continue
		}
		records := auctionTargetRecords(row)
		if maxActions > 0 && planned > 0 && planned+records > maxActions {
			continue
		}
		selected = append(selected, row)
		planned += records
		if maxActions > 0 && planned >= maxActions {
			break
		}
	}
	a.mu.Unlock()
	return selected, nil
}

func (a *App) auctionQueueCandidates(pvfReady bool, catalog map[uint32]catalogItem) ([]uint32, string, error) {
	if pvfReady {
		return catalogAuctionIDs(catalog), "pvf", nil
	}
	rows, err := a.fallbackAuctionRows()
	if err != nil {
		return nil, "", err
	}
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		if row.ItemID != 0 {
			ids = append(ids, row.ItemID)
		}
	}
	return ids, "fallback", nil
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
	ids := catalogAuctionIDs(catalog)
	rows := make([]restockRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := a.catalogAuctionRow(catalog[id]); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func catalogAuctionIDs(catalog map[uint32]catalogItem) []uint32 {
	ids := make([]uint32, 0, len(catalog))
	for id, item := range catalog {
		if marketCandidate(item) {
			ids = append(ids, id)
		}
	}
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
	return ids
}

func auctionTargetRecords(row restockRow) int {
	stackSize := row.StackSize
	if stackSize <= 0 {
		stackSize = 1
	}
	if row.Kind == "equipment" {
		stackSize = 1
	}
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
			Source:      "fallback_seed",
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
		Source:      "pvf",
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

func marketCandidate(item catalogItem) bool {
	return item.ItemID != 0 && item.Kind != "blocked" && !isRiskyPVFItem(item)
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
		if (*dst)[key] == "" {
			(*dst)[key] = value
		}
	}
}

func defaultRestockComments() map[string]string {
	return map[string]string{
		"_summary":              "Auction restock uses PVF candidates minus current DB stock. If PVF export is unavailable, embedded base IDs and prices are used as fallback.",
		"stack_sizes":           "Stackable item count candidates. The actual count is clamped by PVF stack_limit when available.",
		"equipment_qty_min":     "Minimum auction records generated for each missing equipment item.",
		"equipment_qty_max":     "Maximum auction records generated for each missing equipment item.",
		"equipment_inflate_min": "Equipment base price multiplier lower bound.",
		"equipment_inflate_max": "Equipment base price multiplier upper bound.",
		"upgrade_min":           "Minimum random equipment upgrade value written to the auction packet.",
		"upgrade_max":           "Maximum random equipment upgrade value written to the auction packet.",
		"refine_min":            "Minimum random equipment ExtraAddInfo value written to the auction packet.",
		"refine_max":            "Maximum random equipment ExtraAddInfo value written to the auction packet.",
		"upgrade_price_rate":    "Price increase per upgrade level.",
		"refine_price_rate":     "Price increase per ExtraAddInfo level.",
		"rand_low":              "Final price random lower multiplier.",
		"rand_high":             "Final price random upper multiplier.",
		"max_actions":           "Maximum actions per restock round. Default is 10000; use 0 to send the full DB gap.",
		"max_concurrent":        "Concurrent auction register workers.",
		"max_result_actions":    "Maximum action details retained in job result to keep UI/log payload bounded.",
		"per_item_delay_ms":     "Optional delay between actions in each worker. 0 means no intentional delay.",
	}
}
func defaultCeraComments() map[string]string {
	return map[string]string{
		"_summary":      "Gold consignment uses the fixed item list below. PVF is used only to verify item existence when PVF export is available.",
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
		{ItemID: 2675347, Label: "3000w_gold", RestockPrice: 6000, RestockQty: 20, RecyclePrice: 6000, Enabled: false},
	}
}

// ---- workers.go ----
type actionTask struct {
	index  int
	action Action
}

func (a *App) executeActions(jobID string, actions []Action, maxConcurrent int, continueOnError bool, job *JobSummary) (int, []ActionEntry, error) {
	if len(actions) == 0 {
		return 0, nil, nil
	}
	workers := maxConcurrent
	if workers <= 0 {
		workers = a.cfg.Restock.MaxConcurrent
	}
	if workers <= 0 {
		workers = 32
	}
	if workers > len(actions) {
		workers = len(actions)
	}
	delay := time.Duration(a.cfg.Restock.PerItemDelayMS) * time.Millisecond
	resultLimit := a.cfg.Restock.MaxResultActions
	if resultLimit <= 0 {
		resultLimit = 200
	}

	tasks := make(chan actionTask)
	stop := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	var mu lockhub.Locker
	failed := 0
	entries := make([]ActionEntry, 0, len(actions))
	var firstErr error

	record := func(entry ActionEntry, err error) {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, entry)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		} else if !entry.OK {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("action rejected reason=%v", byteValue(entry.Reason))
			}
		}
		if len(job.Actions) < resultLimit {
			job.Actions = append(job.Actions, entry)
		}
		if !continueOnError && firstErr != nil {
			stopOnce.Do(func() { close(stop) })
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			executor := a.executors.NewActionExecutor(a.cfg)
			defer executor.Close()
			for task := range tasks {
				select {
				case <-stop:
					return
				default:
				}
				entry := ActionEntry{Index: task.index, Action: task.action}
				res, err := executor.Execute(task.action)
				if err != nil {
					entry.Error = err.Error()
					a.appendLog(LogEvent{Type: "action", JobID: jobID, Market: task.action.Market, ItemID: task.action.ItemID, OK: boolPtr(false), Message: err.Error()})
					record(entry, err)
				} else {
					entry.OK = res.ResultOK != nil && *res.ResultOK
					entry.AuctionID = res.AuctionID
					if task.action.Operation != "collect" && entry.AuctionID == 0 {
						entry.OK = false
					}
					entry.Reason = res.ResultReason
					entry.Result = res.Raw
					a.appendLog(LogEvent{Type: "action", JobID: jobID, Market: task.action.Market, ItemID: task.action.ItemID, AuctionID: res.AuctionID, OK: &entry.OK, Reason: byteValue(entry.Reason)})
					record(entry, nil)
				}
				if delay > 0 {
					select {
					case <-time.After(delay):
					case <-stop:
						return
					}
				}
			}
		}()
	}

sendLoop:
	for i, action := range actions {
		select {
		case <-stop:
			break sendLoop
		case tasks <- actionTask{index: i, action: action}:
		}
		select {
		case <-stop:
			break sendLoop
		default:
		}
	}
	close(tasks)
	wg.Wait()
	return failed, entries, firstErr
}
