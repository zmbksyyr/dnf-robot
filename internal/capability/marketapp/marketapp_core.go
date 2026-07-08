package marketapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
	"strings"
	"time"
)

// ---- app.go ----
type App struct {
	repository Repository
	cfg        Config
	configPath string
	configDir  string
	pvfPath    string
	dfGameR    string
	executors  ActionExecutorFactory
	restarter  func(name, reason string)

	stateMu   lockhub.Locker
	jobMu     lockhub.Locker
	autoMu    lockhub.Locker
	autoRun   bool
	autoStop  bool
	lastJob   *JobSummary
	dbInit    []string
	dbInitErr string
	itemInfo  ItemInfoSyncStatus
	services  map[string]MarketServiceStatus
	rand      *rand.Rand
	stopAuto  chan struct{}
	autoDone  chan struct{}

	auctionQueue        []uint32
	auctionSpecialQueue []uint32
	auctionRejected     []uint32
	auctionRejectedMeta map[uint32]auctionRejectedState
	auctionRejectedTick int
	auctionQueueSource  string
	ceraRejected        map[uint32]string
	ceraRejectedAt      map[uint32]time.Time
	specialAddInfo      int32
	policy              map[string]MarketPolicyStatus
	lastServiceRestart  map[string]time.Time
}

type auctionRejectedState struct {
	Reason string
	Count  int
	First  time.Time
	Last   time.Time
}

type auctionQueueBudget struct {
	Normal   int
	Special  int
	Rejected int
}

type auctionQueueCounts struct {
	Normal   int
	Special  int
	Rejected int
}

type auctionQueueSelection struct {
	Rows     []restockRow
	Budget   auctionQueueBudget
	Selected auctionQueueCounts
}

type auctionQueueCandidatesResult struct {
	Normal  []uint32
	Special []uint32
	Source  string
}

type auctionQueueSnapshot struct {
	Normal          int
	Special         int
	Rejected        int
	RejectedTracked int
	RejectedRetryIn int
	RejectedReasons string
	Source          string
}

type Planner interface {
	Plan(req RestockRequest) (PlanResult, error)
	CollectPlan(req CollectRequest) (PlanResult, error)
}

type Worker interface {
	RestockOnce(req RestockRequest) (JobSummary, error)
	CollectOnce(req CollectRequest) (JobSummary, error)
}

type Repository interface {
	EnsureMarketTables(dbNames []string, now time.Time) ([]string, error)
	LoadCollectRows(dbName, market string, systemOwnerBase uint32, includeSystemOwners bool) ([]collectRow, error)
	LoadSystemCollectRows(dbName, market string, systemOwnerBase uint32) ([]collectRow, error)
	LoadMarketStock(dbName string, systemOwnerBase uint32, occupied map[uint32]int) (map[uint32]int, error)
	LoadMaxAddInfo(dbName string, min int32) (int32, error)
	CreateCreatureItem(dbName string, ownerID uint32, itemID uint32) (int32, error)
	CountSystemStock(dbName string, systemOwnerBase uint32) (int, error)
	DeleteSystemStock(dbName string, systemOwnerBase uint32) (int64, error)
	CountSystemCreatureItems(dbName string, systemOwnerBase uint32) (int, error)
	DeleteSystemCreatureItems(dbName string, systemOwnerBase uint32) (int64, error)
}

type SQLRepository struct {
	db *sql.DB
}

type appPlanner struct {
	app *App
}

type appWorker struct {
	app *App
}

func (a *App) Planner() Planner {
	return appPlanner{app: a}
}

func (a *App) Worker() Worker {
	return appWorker{app: a}
}

func (p appPlanner) Plan(req RestockRequest) (PlanResult, error) {
	return p.app.Plan(req)
}

func (p appPlanner) CollectPlan(req CollectRequest) (PlanResult, error) {
	return p.app.CollectPlan(req)
}

func (w appWorker) RestockOnce(req RestockRequest) (JobSummary, error) {
	return w.app.RestockOnce(req)
}

func (w appWorker) CollectOnce(req CollectRequest) (JobSummary, error) {
	return w.app.CollectOnce(req)
}

func New(db *sql.DB, sys *config.SysConfig, executors ActionExecutorFactory) (*App, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	if sys == nil {
		return nil, errors.New("nil system config")
	}
	if executors == nil {
		executors = unsupportedActionExecutorFactory{}
	}
	cfg, path, err := LoadConfig(sys.ConfigDir)
	if err != nil {
		return nil, err
	}
	app := &App{
		repository:         SQLRepository{db: db},
		cfg:                cfg,
		configPath:         path,
		configDir:          sys.ConfigDir,
		pvfPath:            filepath.Join(filepath.Dir(sys.DFGameR), "Script.pvf"),
		dfGameR:            sys.DFGameR,
		executors:          executors,
		services:           map[string]MarketServiceStatus{},
		policy:             map[string]MarketPolicyStatus{},
		lastServiceRestart: map[string]time.Time{},
		rand:               rand.New(rand.NewSource(time.Now().UnixNano())),
		stopAuto:           make(chan struct{}),
		autoDone:           make(chan struct{}),
	}
	app.itemInfo = app.itemInfoStatus()
	if tables, err := app.repository.EnsureMarketTables(app.marketDBNames(), time.Now()); err != nil {
		app.dbInit = tables
		app.dbInitErr = err.Error()
		app.appendLog(LogEvent{Type: "db_init", Status: marketLogStatusFailed, Message: err.Error()})
	} else {
		app.dbInit = tables
		app.appendLog(LogEvent{Type: "db_init", Status: marketLogStatusSuccess, Message: strings.Join(tables, ",")})
	}
	app.refreshMarketServiceStatuses()
	return app, nil
}

func (a *App) Config() Config {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.cfg
}

func (a *App) Status() Status {
	a.refreshMarketServiceStatuses()
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return Status{
		ConfigPath:  a.configPath,
		LogPath:     marketLogPath(a.configDir),
		ListenAddr:  a.cfg.ListenAddr,
		Auto:        a.cfg.Auto,
		Collector:   a.cfg.Collector,
		Restock:     a.cfg.Restock,
		AutoRunning: a.AutoRunning(),
		Ready:       true,
		DBInit:      append([]string(nil), a.dbInit...),
		DBInitError: a.dbInitErr,
		ItemInfo:    a.itemInfo,
		Services:    cloneServiceStatusMap(a.services),
		Policy:      clonePolicyStatusMap(a.policy),
		LastJob:     compactJob(a.lastJob),
	}
}

func (a *App) SetAutoEnabled(enabled bool) (Status, error) {
	a.stateMu.Lock()
	a.cfg.Auto.Enabled = enabled
	err := writeJSONFile(a.configPath, a.cfg)
	a.stateMu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	if enabled {
		a.StartAuto()
	} else {
		a.StopAutoAsync()
	}
	return a.Status(), nil
}

func (a *App) UpdateConfig(req ConfigUpdateRequest) (Status, error) {
	a.stateMu.Lock()
	if req.AutoEnabled != nil {
		a.cfg.Auto.Enabled = *req.AutoEnabled
	}
	if req.CollectorEnabled != nil {
		a.cfg.Collector.Enabled = *req.CollectorEnabled
	}
	if req.IntervalMS > 0 {
		a.cfg.Auto.IntervalMS = req.IntervalMS
	}
	if req.InitialDelayMS != nil && *req.InitialDelayMS >= 0 {
		a.cfg.Auto.InitialDelayMS = *req.InitialDelayMS
	}
	if req.MaxActions >= 0 {
		a.cfg.Auto.MaxActions = req.MaxActions
		a.cfg.Collector.MaxActions = req.MaxActions
		a.cfg.Restock.MaxActions = req.MaxActions
	}
	if req.MaxConcurrent > 0 {
		a.cfg.Auto.MaxConcurrent = req.MaxConcurrent
		a.cfg.Collector.MaxConcurrent = req.MaxConcurrent
		a.cfg.Restock.MaxConcurrent = req.MaxConcurrent
	}
	if req.ContinueOnError != nil {
		a.cfg.Auto.ContinueOnError = *req.ContinueOnError
	}
	if len(req.Markets) > 0 {
		a.cfg.Auto.Markets = req.Markets
	}
	a.cfg.applyDefaults()
	cfg := a.cfg
	err := writeJSONFile(a.configPath, cfg)
	a.stateMu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	if cfg.Auto.Enabled {
		a.RestartAutoAsync()
	} else {
		a.StopAutoAsync()
	}
	if cfg.Auto.Enabled && !a.AutoRunning() {
		a.StartAuto()
	}
	return a.Status(), nil
}

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
	selection, err := a.nextAuctionQueueSelection(pvfReady, catalog, haveAuction, maxActions)
	if err != nil {
		return err
	}
	rows := selection.Rows
	decision.SelectedAuctionRows = len(rows)
	decision.AuctionBudget = selection.Budget
	decision.AuctionSelected = selection.Selected
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

func busyMarketJob(kind string) JobSummary {
	now := time.Now()
	return JobSummary{
		ID:        fmt.Sprintf("%s-busy-%d", kind, now.UnixNano()),
		Kind:      kind,
		Status:    MarketJobStatusBusy,
		Error:     "market job already running",
		StartedAt: now,
		EndedAt:   now,
	}
}

func (a *App) RestockOnce(req RestockRequest) (JobSummary, error) {
	if !a.jobMu.TryLock() {
		job := busyMarketJob("restock")
		return job, fmt.Errorf(job.Error)
	}
	defer a.jobMu.Unlock()
	if req.MaxActions <= 0 {
		req.MaxActions = a.cfg.Restock.MaxActions
	}
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("restock-%d", start.UnixNano()),
		Kind:      "restock",
		Status:    MarketJobStatusRunning,
		StartedAt: start,
	}
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.Plan(req)
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
		maxActions = a.cfg.Restock.MaxActions
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
	a.reconcileCeraRejects(entries)
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
		a.applyRestockDBConfirmation(&job, actions)
	}
	job.EndedAt = time.Now()
	job.Duration = job.EndedAt.Sub(job.StartedAt).Milliseconds()
	a.setLastJob(job)
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
	return job, nil
}

func (a *App) applyRestockDBConfirmation(job *JobSummary, actions []Action) {
	if !needsAuctionDBConfirmation(actions) {
		job.Status = MarketJobStatusSuccess
		job.Error = ""
		return
	}
	confirmed, err := a.auctionDBConfirmed(actions)
	if err != nil {
		job.Status = MarketJobStatusPartialFailed
		job.Error = fmt.Sprintf("auction db confirmation failed: %v", err)
		return
	}
	if !confirmed {
		job.Status = MarketJobStatusPendingDB
		job.Error = "auction register acked; waiting for DB fact confirmation"
		return
	}
	job.Status = MarketJobStatusSuccess
	job.Error = ""
}

func needsAuctionDBConfirmation(actions []Action) bool {
	for _, action := range actions {
		if action.Market == marketNameAuction && action.Operation != "collect" {
			return true
		}
	}
	return false
}

func (a *App) auctionDBConfirmed(actions []Action) (bool, error) {
	watch := map[uint32]bool{}
	for _, action := range actions {
		if action.Market == marketNameAuction && action.Operation != "collect" && action.ItemID > 0 {
			watch[action.ItemID] = true
		}
	}
	if len(watch) == 0 {
		return true, nil
	}
	have, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, map[uint32]int{})
	if err != nil {
		return false, err
	}
	for itemID := range watch {
		if have[itemID] > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) setLastJob(job JobSummary) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.lastJob = &job
}

func compactJob(job *JobSummary) *JobSummary {
	if job == nil {
		return nil
	}
	out := *job
	out.Actions = nil
	return &out
}

func cloneServiceStatusMap(in map[string]MarketServiceStatus) map[string]MarketServiceStatus {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]MarketServiceStatus, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func clonePolicyStatusMap(in map[string]MarketPolicyStatus) map[string]MarketPolicyStatus {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]MarketPolicyStatus, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (a *App) loadCatalog() (map[uint32]catalogItem, error) {
	out := map[uint32]catalogItem{}
	stackable, err := readPVFItems(filepath.Join(a.configDir, "pvf_stackable_catalog.json"))
	if err != nil {
		return nil, err
	}
	for _, item := range stackable {
		if item.ID <= 0 {
			continue
		}
		kind := "stackable"
		if item.BadName || item.NoTrade || item.Expire {
			kind = "blocked"
		}
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, Level: item.Level, ItemType: item.ItemType, SubType: item.SubType, Slot: item.Slot, Attach: item.Attach, Rarity: item.Rarity, StackLimit: item.StackLimit, Price: int32(item.Price), Value: int32(item.Value)}
	}
	equipment, err := readPVFItems(filepath.Join(a.configDir, "pvf_equipment_catalog.json"))
	if err != nil {
		return nil, err
	}
	for _, item := range equipment {
		if item.ID <= 0 {
			continue
		}
		kind := "equipment"
		if item.BadName || item.NoTrade || item.Expire {
			kind = "blocked"
		}
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, Level: item.Level, ItemType: item.ItemType, SubType: item.SubType, Slot: item.Slot, Attach: item.Attach, Rarity: item.Rarity, StackLimit: item.StackLimit, Price: int32(item.Price), Value: int32(item.Value)}
	}
	return out, nil
}

func readPVFItems(path string) ([]pvfItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var items []pvfItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// ---- config.go ----
func DefaultConfig() Config {
	return Config{
		ListenAddr:         "0.0.0.0:8121",
		FridaDB:            "frida",
		GameDB:             "taiwan_cain_2nd",
		AuctionDB:          "taiwan_cain_auction_gold",
		CeraDB:             "taiwan_cain_auction_cera",
		AuctionHost:        "127.0.0.1",
		AuctionPort:        30803,
		CeraHost:           "127.0.0.1",
		CeraPort:           30603,
		ItemInfoSourcePath: "pvf_iteminfo.dat",
		ItemInfoTargets: []string{
			"/home/neople/auction/iteminfo.dat",
			"/home/neople/point/iteminfo.dat",
			"/home/dxf/auction/iteminfo.dat",
			"/home/dxf/point/iteminfo.dat",
		},
		AutoSyncItemInfo: false,
		SystemOwner: SystemOwner{
			IDBase:      90000001,
			BuyerBase:   90100001,
			NexonBase:   18000000,
			OwnerName:   "market",
			CeraName:    "gold",
			RotateEvery: 10,
		},
		Collector: CollectorCfg{
			Enabled:          true,
			MaxActions:       0,
			MaxConcurrent:    8,
			MaxResultActions: 200,
			PerItemDelayMS:   0,
		},
		Restock: RestockCfg{
			Comments:         defaultRestockComments(),
			StackSizes:       []int{500, 1000, 2000},
			EquipmentQtyMin:  2,
			EquipmentQtyMax:  5,
			EquipInflateMin:  5,
			EquipInflateMax:  8,
			UpgradeMin:       7,
			UpgradeMax:       13,
			UpgradePriceRate: 0.08,
			RandLow:          0.9,
			RandHigh:         1.1,
			MaxActions:       10000,
			MaxConcurrent:    8,
			MaxResultActions: 200,
			PerItemDelayMS:   0,
		},
		Cera: CeraCfg{
			Comments: defaultCeraComments(),
			Items:    defaultCeraRows(),
		},
		Auto: AutoCfg{
			Enabled:         false,
			Markets:         []string{marketNameAuction, marketNameCera},
			InitialDelayMS:  3000,
			IntervalMS:      60000,
			MaxActions:      10000,
			MaxConcurrent:   8,
			ContinueOnError: true,
		},
	}
}

func LoadConfig(configDir string) (Config, string, error) {
	cfg := DefaultConfig()
	path := filepath.Join(configDir, "market_config.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeJSONFile(path, cfg); err != nil {
			return cfg, path, err
		}
		return cfg, path, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, path, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, path, err
	}
	cfg.applyDefaults()
	if err := writeJSONFile(path, cfg); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}

func (c *Config) applyDefaults() {
	d := DefaultConfig()
	autoUnset := !c.Auto.Enabled && len(c.Auto.Markets) == 0 && c.Auto.InitialDelayMS == 0 && c.Auto.IntervalMS == 0 && c.Auto.MaxActions == 0 && c.Auto.MaxConcurrent == 0 && !c.Auto.ContinueOnError
	collectorUnset := !c.Collector.Enabled && c.Collector.MaxActions == 0 && c.Collector.MaxConcurrent == 0 && c.Collector.MaxResultActions == 0 && c.Collector.PerItemDelayMS == 0 && !c.Collector.IncludeSystemOwners
	if c.ListenAddr == "" {
		c.ListenAddr = d.ListenAddr
	}
	if c.FridaDB == "" {
		c.FridaDB = d.FridaDB
	}
	if c.GameDB == "" {
		c.GameDB = d.GameDB
	}
	if c.AuctionDB == "" {
		c.AuctionDB = d.AuctionDB
	}
	if c.CeraDB == "" {
		c.CeraDB = d.CeraDB
	}
	if c.AuctionHost == "" {
		c.AuctionHost = d.AuctionHost
	}
	if c.AuctionPort == 0 {
		c.AuctionPort = d.AuctionPort
	}
	if c.CeraHost == "" {
		c.CeraHost = d.CeraHost
	}
	if c.CeraPort == 0 {
		c.CeraPort = d.CeraPort
	}
	if c.ItemInfoSourcePath == "" {
		c.ItemInfoSourcePath = d.ItemInfoSourcePath
	}
	if len(c.ItemInfoTargets) == 0 {
		c.ItemInfoTargets = d.ItemInfoTargets
	}
	if c.SystemOwner.IDBase == 0 {
		c.SystemOwner.IDBase = d.SystemOwner.IDBase
	}
	if c.SystemOwner.BuyerBase == 0 {
		c.SystemOwner.BuyerBase = d.SystemOwner.BuyerBase
	}
	if c.SystemOwner.NexonBase == 0 {
		c.SystemOwner.NexonBase = d.SystemOwner.NexonBase
	}
	if c.SystemOwner.OwnerName == "" {
		c.SystemOwner.OwnerName = d.SystemOwner.OwnerName
	}
	if c.SystemOwner.CeraName == "" {
		c.SystemOwner.CeraName = d.SystemOwner.CeraName
	}
	if c.SystemOwner.RotateEvery <= 0 {
		c.SystemOwner.RotateEvery = d.SystemOwner.RotateEvery
	}
	if c.Collector.MaxConcurrent <= 0 {
		c.Collector.MaxConcurrent = d.Collector.MaxConcurrent
	}
	if c.Collector.MaxResultActions <= 0 {
		c.Collector.MaxResultActions = d.Collector.MaxResultActions
	}
	if c.Collector.PerItemDelayMS < 0 {
		c.Collector.PerItemDelayMS = d.Collector.PerItemDelayMS
	}
	if collectorUnset {
		c.Collector.Enabled = d.Collector.Enabled
	}
	mergeStringMap(&c.Restock.Comments, d.Restock.Comments)
	if len(c.Restock.StackSizes) == 0 {
		c.Restock.StackSizes = d.Restock.StackSizes
	}
	if c.Restock.EquipmentQtyMin <= 0 {
		c.Restock.EquipmentQtyMin = d.Restock.EquipmentQtyMin
	}
	if c.Restock.EquipmentQtyMax < c.Restock.EquipmentQtyMin {
		c.Restock.EquipmentQtyMax = c.Restock.EquipmentQtyMin
	}
	if c.Restock.EquipInflateMin <= 0 {
		c.Restock.EquipInflateMin = d.Restock.EquipInflateMin
	}
	if c.Restock.EquipInflateMax < c.Restock.EquipInflateMin {
		c.Restock.EquipInflateMax = c.Restock.EquipInflateMin
	}
	if c.Restock.UpgradeMin <= 0 {
		c.Restock.UpgradeMin = d.Restock.UpgradeMin
	}
	if c.Restock.UpgradeMax < c.Restock.UpgradeMin {
		c.Restock.UpgradeMax = c.Restock.UpgradeMin
	}
	if c.Restock.UpgradePriceRate <= 0 {
		c.Restock.UpgradePriceRate = d.Restock.UpgradePriceRate
	}
	if c.Restock.RandLow <= 0 || c.Restock.RandLow == 1 && c.Restock.RandHigh == 1 {
		c.Restock.RandLow = d.Restock.RandLow
	}
	if c.Restock.RandHigh <= 0 || c.Restock.RandLow == d.Restock.RandLow && c.Restock.RandHigh == 1 {
		c.Restock.RandHigh = d.Restock.RandHigh
	}
	if c.Restock.RandHigh < c.Restock.RandLow {
		c.Restock.RandHigh = c.Restock.RandLow
	}
	if c.Restock.MaxConcurrent <= 0 {
		c.Restock.MaxConcurrent = d.Restock.MaxConcurrent
	}
	if c.Restock.MaxResultActions <= 0 {
		c.Restock.MaxResultActions = d.Restock.MaxResultActions
	}
	if c.Restock.PerItemDelayMS < 0 {
		c.Restock.PerItemDelayMS = d.Restock.PerItemDelayMS
	}
	if len(c.Cera.Items) == 0 {
		c.Cera.Items = d.Cera.Items
	}
	for i := range c.Cera.Items {
		if c.Cera.Items[i].ItemID == 2675347 && c.Cera.Items[i].Label == "3000w_gold" {
			c.Cera.Items[i].Enabled = true
		}
	}
	mergeStringMap(&c.Cera.Comments, d.Cera.Comments)
	if len(c.Auto.Markets) == 0 {
		c.Auto.Markets = d.Auto.Markets
	}
	if c.Auto.InitialDelayMS < 0 {
		c.Auto.InitialDelayMS = d.Auto.InitialDelayMS
	}
	if c.Auto.IntervalMS < 60000 {
		c.Auto.IntervalMS = d.Auto.IntervalMS
	}
	if c.Auto.MaxConcurrent <= 0 {
		c.Auto.MaxConcurrent = d.Auto.MaxConcurrent
	}
	if autoUnset {
		c.Auto.Enabled = d.Auto.Enabled
		c.Auto.ContinueOnError = d.Auto.ContinueOnError
	}
}

func writeJSONFile(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// ---- types.go ----
type Config struct {
	ListenAddr         string       `json:"listen_addr"`
	FridaDB            string       `json:"frida_db"`
	GameDB             string       `json:"game_db"`
	AuctionDB          string       `json:"auction_db"`
	CeraDB             string       `json:"cera_db"`
	AuctionHost        string       `json:"auction_host"`
	AuctionPort        int          `json:"auction_port"`
	CeraHost           string       `json:"cera_host"`
	CeraPort           int          `json:"cera_port"`
	ItemInfoSourcePath string       `json:"iteminfo_source_path"`
	ItemInfoTargets    []string     `json:"iteminfo_targets"`
	AutoSyncItemInfo   bool         `json:"auto_sync_iteminfo"`
	SystemOwner        SystemOwner  `json:"system_owner"`
	Collector          CollectorCfg `json:"collector"`
	Restock            RestockCfg   `json:"restock"`
	Cera               CeraCfg      `json:"cera"`
	Auto               AutoCfg      `json:"auto"`
}

type SystemOwner struct {
	IDBase      uint32 `json:"id_base"`
	BuyerBase   uint32 `json:"buyer_base"`
	NexonBase   uint32 `json:"nexon_base"`
	OwnerName   string `json:"owner_name"`
	CeraName    string `json:"cera_name"`
	RotateEvery int    `json:"rotate_every"`
}

type RestockCfg struct {
	Comments         map[string]string `json:"comments,omitempty"`
	StackSizes       []int             `json:"stack_sizes"`
	EquipmentQtyMin  int               `json:"equipment_qty_min"`
	EquipmentQtyMax  int               `json:"equipment_qty_max"`
	EquipInflateMin  int               `json:"equipment_inflate_min"`
	EquipInflateMax  int               `json:"equipment_inflate_max"`
	UpgradeMin       int               `json:"upgrade_min"`
	UpgradeMax       int               `json:"upgrade_max"`
	UpgradePriceRate float64           `json:"upgrade_price_rate"`
	RandLow          float64           `json:"rand_low"`
	RandHigh         float64           `json:"rand_high"`
	MaxActions       int               `json:"max_actions"`
	MaxConcurrent    int               `json:"max_concurrent"`
	MaxResultActions int               `json:"max_result_actions"`
	PerItemDelayMS   int               `json:"per_item_delay_ms"`
}

type CeraCfg struct {
	Comments map[string]string `json:"comments,omitempty"`
	Items    []ceraRow         `json:"items"`
}

type CollectorCfg struct {
	Enabled             bool `json:"enabled"`
	MaxActions          int  `json:"max_actions"`
	MaxConcurrent       int  `json:"max_concurrent"`
	MaxResultActions    int  `json:"max_result_actions"`
	PerItemDelayMS      int  `json:"per_item_delay_ms"`
	IncludeSystemOwners bool `json:"include_system_owners"`
}

type AutoCfg struct {
	Enabled         bool     `json:"enabled"`
	Markets         []string `json:"markets"`
	InitialDelayMS  int      `json:"initial_delay_ms"`
	IntervalMS      int      `json:"interval_ms"`
	MaxActions      int      `json:"max_actions"`
	MaxConcurrent   int      `json:"max_concurrent"`
	ContinueOnError bool     `json:"continue_on_error"`
}

type RestockRequest struct {
	Market          string `json:"market,omitempty"`
	Execute         bool   `json:"execute,omitempty"`
	MaxActions      int    `json:"max_actions,omitempty"`
	MaxConcurrent   int    `json:"max_concurrent,omitempty"`
	ContinueOnError bool   `json:"continue_on_error,omitempty"`
}

type CollectRequest struct {
	Market          string `json:"market,omitempty"`
	Execute         bool   `json:"execute,omitempty"`
	MaxActions      int    `json:"max_actions,omitempty"`
	MaxConcurrent   int    `json:"max_concurrent,omitempty"`
	ContinueOnError bool   `json:"continue_on_error,omitempty"`
}

type AuctionSearchGuardRequest struct {
	Path string `json:"path,omitempty"`
}

type AuctionSearchGuardResult struct {
	Path      string `json:"path"`
	Backup    string `json:"backup,omitempty"`
	Installed bool   `json:"installed"`
	Changed   bool   `json:"changed"`
	Message   string `json:"message,omitempty"`
}

type AuctionMemoryPatchRequest struct{}

type AuctionMemoryPatchResult struct {
	PID     int                       `json:"pid"`
	Target  string                    `json:"target"`
	Patched int                       `json:"patched"`
	Entries []AuctionMemoryPatchEntry `json:"entries"`
}

type AuctionMemoryPatchEntry struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Before  byte   `json:"before"`
	After   byte   `json:"after"`
	Expect  byte   `json:"expect"`
	Value   byte   `json:"value"`
	Changed bool   `json:"changed"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type PVFUpgradeSeparateRequest struct {
	Path   string `json:"path,omitempty"`
	Target int    `json:"target,omitempty"`
}

type ClearSystemStockResult struct {
	Markets []ClearSystemMarketResult `json:"markets"`
	Deleted int64                     `json:"deleted"`
}

type ClearSystemMarketResult struct {
	Market  string `json:"market"`
	DBName  string `json:"db_name"`
	Before  int    `json:"before"`
	Deleted int64  `json:"deleted"`
	After   int    `json:"after"`
	Status  string `json:"status"`
}

type ConfigUpdateRequest struct {
	AutoEnabled      *bool    `json:"auto_enabled,omitempty"`
	CollectorEnabled *bool    `json:"collector_enabled,omitempty"`
	IntervalMS       int      `json:"interval_ms,omitempty"`
	InitialDelayMS   *int     `json:"initial_delay_ms,omitempty"`
	MaxActions       int      `json:"max_actions,omitempty"`
	MaxConcurrent    int      `json:"max_concurrent,omitempty"`
	ContinueOnError  *bool    `json:"continue_on_error,omitempty"`
	Markets          []string `json:"markets,omitempty"`
}

type Status struct {
	ConfigPath  string                         `json:"config_path"`
	LogPath     string                         `json:"log_path"`
	ListenAddr  string                         `json:"listen_addr"`
	Auto        AutoCfg                        `json:"auto"`
	Collector   CollectorCfg                   `json:"collector"`
	Restock     RestockCfg                     `json:"restock"`
	AutoRunning bool                           `json:"auto_running"`
	Ready       bool                           `json:"ready"`
	DBInit      []string                       `json:"db_init,omitempty"`
	DBInitError string                         `json:"db_init_error,omitempty"`
	ItemInfo    ItemInfoSyncStatus             `json:"iteminfo"`
	Services    map[string]MarketServiceStatus `json:"services,omitempty"`
	Policy      map[string]MarketPolicyStatus  `json:"policy,omitempty"`
	LastJob     *JobSummary                    `json:"last_job,omitempty"`
	LogTail     []LogEvent                     `json:"log_tail,omitempty"`
}

type MarketPolicyStatus struct {
	Market               string    `json:"market"`
	Health               string    `json:"health"`
	Completion           int       `json:"completion"`
	Mode                 string    `json:"mode"`
	Reason               string    `json:"reason,omitempty"`
	DBKinds              int       `json:"db_kinds"`
	KindDelta            int       `json:"kind_delta,omitempty"`
	Candidates           int       `json:"candidates,omitempty"`
	SpecialCandidates    int       `json:"special_candidates,omitempty"`
	CandidateSource      string    `json:"candidate_source,omitempty"`
	ZeroKindRounds       int       `json:"zero_kind_rounds,omitempty"`
	ZeroCandidateRounds  int       `json:"zero_candidate_rounds,omitempty"`
	StagnantRounds       int       `json:"stagnant_rounds,omitempty"`
	ActionFailureRounds  int       `json:"action_failure_rounds,omitempty"`
	QueueNormal          int       `json:"queue_normal,omitempty"`
	QueueSpecial         int       `json:"queue_special,omitempty"`
	QueueRejected        int       `json:"queue_rejected,omitempty"`
	QueueRejectedTracked int       `json:"queue_rejected_tracked,omitempty"`
	QueueRejectedRetryIn int       `json:"queue_rejected_retry_in,omitempty"`
	QueueSource          string    `json:"queue_source,omitempty"`
	EffectiveMaxActions  int       `json:"effective_max_actions"`
	EffectiveConcurrency int       `json:"effective_concurrency"`
	LastJobStatus        string    `json:"last_job_status,omitempty"`
	LastJobError         string    `json:"last_job_error,omitempty"`
	LastPlanActions      int       `json:"last_plan_actions,omitempty"`
	LastActionResults    int       `json:"last_action_results,omitempty"`
	LastActionFailed     int       `json:"last_action_failed,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

type ItemInfoSyncStatus struct {
	SourcePath string   `json:"source_path,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	Synced     int      `json:"synced"`
	Skipped    int      `json:"skipped"`
	Error      string   `json:"error,omitempty"`
}

type MarketServiceStatus struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Addr      string    `json:"addr"`
	Dir       string    `json:"dir"`
	Bin       string    `json:"bin"`
	PID       int       `json:"pid,omitempty"`
	Listening bool      `json:"listening"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	LogPath   string    `json:"log_path,omitempty"`
	Message   string    `json:"message,omitempty"`
}

const (
	MarketServiceStatusReady                   = "ready"
	MarketServiceStatusDown                    = "down"
	MarketServiceStatusPortReadyProcessMissing = "port_ready_process_missing"
	MarketServiceStatusProcessWithoutPort      = "process_without_port"
	MarketServiceStatusPrepareFailed           = "prepare_failed"
	MarketServiceStatusStartFailed             = "start_failed"
	MarketServiceStatusRegistItemFailed        = "regist_item_failed"
	MarketServiceStatusProcessExited           = "process_exited"
	MarketServiceStatusPortReadyButUnstable    = "port_ready_but_unstable"
	MarketServiceStatusStartTimeout            = "start_timeout"
)

type JobSummary struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  int64         `json:"duration_ms,omitempty"`
	Plan      *PlanSummary  `json:"plan,omitempty"`
	Actions   []ActionEntry `json:"actions,omitempty"`
	Error     string        `json:"error,omitempty"`
}

const (
	MarketJobStatusBusy          = "busy"
	MarketJobStatusRunning       = "running"
	MarketJobStatusFailed        = "failed"
	MarketJobStatusPendingDB     = "pending_db_confirm"
	MarketJobStatusPlanned       = "planned"
	MarketJobStatusPartialFailed = "partial_failed"
	MarketJobStatusSuccess       = "success"
)

const (
	marketNameAuction = "auction"
	marketNameCera    = "cera"
)

const (
	marketAliasGold  = "gold"
	marketAliasPoint = "point"
)

const (
	marketServiceNameAuction = "auction"
	marketServiceNamePoint   = "point"
)

const (
	marketQueueSourcePVFItemInfo        = "pvf_iteminfo"
	marketQueueSourcePVFItemInfoMissing = "pvf_iteminfo_missing"
	marketQueueSourceFallback           = "fallback"
	marketRowSourcePVF                  = "pvf"
	marketRowSourceFallbackSeed         = "fallback_seed"
	marketActionSourceUnknown           = "unknown"
	marketActionSourceCeraConfig        = "cera_config"
	marketCandidateSourceUnavailable    = "unavailable"
)

const (
	marketLogStatusActive           = "active"
	marketLogStatusBlocked          = "blocked"
	marketLogStatusClean            = "clean"
	marketLogStatusCountAfterFailed = "count_after_failed"
	marketLogStatusCountFailed      = "count_failed"
	marketLogStatusDBDeleted        = "db_deleted"
	marketLogStatusDeleteFailed     = "delete_failed"
	marketLogStatusDisabled         = "disabled"
	marketLogStatusEmpty            = "empty"
	marketLogStatusExists           = "exists"
	marketLogStatusFailed           = "failed"
	marketLogStatusFallback         = "fallback"
	marketLogStatusGameDown         = "game_down"
	marketLogStatusInstalled        = "installed"
	marketLogStatusKilled           = "killed"
	marketLogStatusQueueReset       = "queue_reset"
	marketLogStatusRestart          = "restart"
	marketLogStatusServiceDown      = "service_down"
	marketLogStatusSkipped          = "skipped"
	marketLogStatusStart            = "start"
	marketLogStatusStopSkipped      = "stop_skipped"
	marketLogStatusStopped          = "stopped"
	marketLogStatusSuccess          = "success"
	marketLogStatusSynced           = "synced"
	marketLogStatusStaleItemInfo    = "stale_iteminfo_restart"
	marketLogStatusWaitFailed       = "wait_failed"
)

type PlanResult struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Summary     PlanSummary   `json:"summary"`
	Actions     []Action      `json:"actions"`
	Skipped     []SkippedItem `json:"skipped,omitempty"`
}

type PlanSummary struct {
	Actions         int `json:"actions"`
	AuctionActions  int `json:"auction_actions"`
	CeraActions     int `json:"cera_actions"`
	Skipped         int `json:"skipped"`
	Missing         int `json:"missing"`
	Risky           int `json:"risky"`
	Special         int `json:"special"`
	NotAuctionable  int `json:"not_auctionable"`
	ExistingRecords int `json:"existing_records"`
}

type Action struct {
	Market       string `json:"market"`
	Kind         string `json:"kind"`
	Operation    string `json:"operation,omitempty"`
	ItemID       uint32 `json:"item_id"`
	ItemType     int    `json:"item_type,omitempty"`
	Name         string `json:"name,omitempty"`
	Count        int32  `json:"count"`
	UnitPrice    int32  `json:"unit_price"`
	TotalPrice   int32  `json:"total_price"`
	OwnerID      uint32 `json:"owner_id"`
	OwnerName    string `json:"owner_name"`
	CountAddInfo int32  `json:"count_or_add_info"`
	StartPrice   int32  `json:"start_price"`
	InstantPrice int32  `json:"instant_price"`
	Upgrade      int    `json:"upgrade,omitempty"`
	ExtraAddInfo int32  `json:"extra_add_info,omitempty"`
	AuctionID    uint64 `json:"auction_id,omitempty"`
	Source       string `json:"source"`
}

type ActionEntry struct {
	Index     int         `json:"index"`
	Action    Action      `json:"action"`
	OK        bool        `json:"ok"`
	AuctionID uint64      `json:"auction_id,omitempty"`
	Reason    *byte       `json:"reason,omitempty"`
	Result    interface{} `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type SkippedItem struct {
	Market string `json:"market"`
	ItemID uint32 `json:"item_id"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

type pvfItem struct {
	ID         int    `json:"id"`
	Level      int    `json:"level,omitempty"`
	ItemType   int    `json:"item_type"`
	SubType    int    `json:"sub_type,omitempty"`
	Slot       string `json:"slot,omitempty"`
	Attach     string `json:"attach,omitempty"`
	Rarity     int    `json:"rarity,omitempty"`
	Price      int    `json:"price,omitempty"`
	Value      int    `json:"value,omitempty"`
	Trade      bool   `json:"trade,omitempty"`
	NoTrade    bool   `json:"no_trade,omitempty"`
	Auction    bool   `json:"auction,omitempty"`
	BadName    bool   `json:"bad_name,omitempty"`
	StackLimit int    `json:"stack_limit,omitempty"`
	Expire     bool   `json:"expire,omitempty"`
}

type catalogItem struct {
	ItemID     uint32
	Name       string
	Kind       string
	Level      int
	ItemType   int
	SubType    int
	Slot       string
	Attach     string
	Rarity     int
	StackLimit int
	Price      int32
	Value      int32
}

type restockRow struct {
	ItemID      uint32 `json:"item_id"`
	Name        string `json:"name"`
	SystemPrice int32  `json:"system_price"`
	Quantity    int    `json:"quantity"`
	StackSize   int    `json:"stack_size"`
	Upgrade     int    `json:"upgrade,omitempty"`
	Endurance   int    `json:"endurance,omitempty"`
	SealFlag    int    `json:"seal_flag,omitempty"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source,omitempty"`
	Kind        string `json:"-"`
	Level       int    `json:"-"`
	ItemType    int    `json:"-"`
	SubType     int    `json:"-"`
	Slot        string `json:"-"`
	Attach      string `json:"-"`
	Rarity      int    `json:"-"`
	StackLimit  int    `json:"-"`
}

func (r restockRow) marketItem() catalogItem {
	kind := r.Kind
	if kind == "" {
		kind = "stackable"
	}
	return catalogItem{
		ItemID:     r.ItemID,
		Name:       r.Name,
		Kind:       kind,
		Level:      r.Level,
		ItemType:   r.ItemType,
		SubType:    r.SubType,
		Slot:       r.Slot,
		Attach:     r.Attach,
		Rarity:     r.Rarity,
		StackLimit: r.StackLimit,
		Price:      r.SystemPrice,
	}
}

func (r *restockRow) applyMarketItem(item catalogItem) {
	r.Name = item.Name
	r.Kind = item.Kind
	r.Level = item.Level
	r.ItemType = item.ItemType
	r.SubType = item.SubType
	r.Slot = item.Slot
	r.Attach = item.Attach
	r.Rarity = item.Rarity
	r.StackLimit = item.StackLimit
	if r.SystemPrice <= 0 {
		r.SystemPrice = marketBasePrice(item)
	}
}

type ceraRow struct {
	ItemID       uint32 `json:"item_id"`
	Label        string `json:"name"`
	RestockPrice int32  `json:"restock_price"`
	RestockQty   int    `json:"restock_qty"`
	RecyclePrice int32  `json:"recycle_price"`
	Enabled      bool   `json:"enabled"`
}

type corePoolItem struct {
	ItemID    uint32 `json:"item_id"`
	BasePrice int32  `json:"base_price"`
}

type LogEvent struct {
	Time          time.Time         `json:"time"`
	Type          string            `json:"type"`
	JobID         string            `json:"job_id,omitempty"`
	Status        string            `json:"status,omitempty"`
	Market        string            `json:"market,omitempty"`
	ItemID        uint32            `json:"item_id,omitempty"`
	AuctionID     uint64            `json:"auction_id,omitempty"`
	OK            *bool             `json:"ok,omitempty"`
	Reason        interface{}       `json:"reason,omitempty"`
	Message       string            `json:"message,omitempty"`
	Summary       *PlanSummary      `json:"summary,omitempty"`
	ActionSummary *ActionLogSummary `json:"action_summary,omitempty"`
}

type ActionLogSummary struct {
	Total      int             `json:"total"`
	OK         int             `json:"ok"`
	Failed     int             `json:"failed"`
	ErrorCount int             `json:"error_count,omitempty"`
	ByMarket   map[string]int  `json:"by_market,omitempty"`
	ByReason   map[string]int  `json:"by_reason,omitempty"`
	TopFailed  []ActionLogItem `json:"top_failed,omitempty"`
}

type ActionLogItem struct {
	ItemID uint32 `json:"item_id"`
	Count  int    `json:"count"`
	Reason string `json:"reason,omitempty"`
}

// ---- errors.go ----
var ErrExecutorUnavailable = errors.New("market action executor unavailable")

// ---- log.go ----
const marketLogFile = "market_log.jsonl"

func marketLogPath(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, marketLogFile)
}

func (a *App) appendLog(event LogEvent) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	path := marketLogPath(a.configDir)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// ---- executor.go ----
type ActionExecutionResult struct {
	ResultOK     *bool
	ResultReason *byte
	AuctionID    uint64
	Raw          interface{}
}

type ActionExecutor interface {
	Execute(action Action) (ActionExecutionResult, error)
	Close()
}

type ActionExecutorFactory interface {
	NewActionExecutor(cfg Config) ActionExecutor
}

type unsupportedActionExecutorFactory struct{}

func (unsupportedActionExecutorFactory) NewActionExecutor(cfg Config) ActionExecutor {
	return unsupportedActionExecutor{}
}

type unsupportedActionExecutor struct{}

func (unsupportedActionExecutor) Execute(action Action) (ActionExecutionResult, error) {
	return ActionExecutionResult{}, ErrExecutorUnavailable
}

func (unsupportedActionExecutor) Close() {}
