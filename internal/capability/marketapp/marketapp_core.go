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

	mu        lockhub.Locker
	jobMu     lockhub.Locker
	autoMu    lockhub.Locker
	autoRun   bool
	lastJob   *JobSummary
	dbInit    []string
	dbInitErr string
	itemInfo  ItemInfoSyncStatus
	rand      *rand.Rand
	stopAuto  chan struct{}
	autoDone  chan struct{}

	auctionQueue       []uint32
	auctionQueueSource string
	auctionPatchPID    int
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
	LoadMarketStock(dbName string, systemOwnerBase uint32, occupied map[uint32]int) (map[uint32]int, error)
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
	cleanupLegacyMarketFiles(sys.ConfigDir)
	app := &App{
		repository: SQLRepository{db: db},
		cfg:        cfg,
		configPath: path,
		configDir:  sys.ConfigDir,
		pvfPath:    filepath.Join(filepath.Dir(sys.DFGameR), "Script.pvf"),
		dfGameR:    sys.DFGameR,
		executors:  executors,
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
		stopAuto:   make(chan struct{}),
		autoDone:   make(chan struct{}),
	}
	app.itemInfo = app.itemInfoStatus()
	if tables, err := app.repository.EnsureMarketTables(app.marketDBNames(), time.Now()); err != nil {
		app.dbInit = tables
		app.dbInitErr = err.Error()
		app.appendLog(LogEvent{Type: "db_init", Status: "failed", Message: err.Error()})
	} else {
		app.dbInit = tables
		app.appendLog(LogEvent{Type: "db_init", Status: "success", Message: strings.Join(tables, ",")})
	}
	app.patchAuctionMemoryIfRunning("init")
	return app, nil
}

func (a *App) Config() Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
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
		LastJob:     compactJob(a.lastJob),
	}
}

func (a *App) SetAutoEnabled(enabled bool) (Status, error) {
	a.mu.Lock()
	a.cfg.Auto.Enabled = enabled
	err := writeJSONFile(a.configPath, a.cfg)
	a.mu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	if enabled {
		a.StartAuto()
	} else {
		a.StopAuto()
	}
	return a.Status(), nil
}

func (a *App) UpdateConfig(req ConfigUpdateRequest) (Status, error) {
	a.mu.Lock()
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
	a.mu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	a.StopAuto()
	if cfg.Auto.Enabled {
		a.StartAuto()
	}
	return a.Status(), nil
}

func (a *App) Plan(req RestockRequest) (PlanResult, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	needAuction := market == "" || market == "auction"
	needCera := market == "" || market == "cera" || market == "gold"
	catalogLoaded := false
	pvfReady := false
	var catalog map[uint32]catalogItem
	if needCera || needAuction {
		var err error
		catalog, err = a.loadCatalog()
		catalogLoaded = true
		pvfReady = err == nil
		if !pvfReady {
			a.appendLog(LogEvent{Type: "pvf_catalog", Status: "fallback", Message: err.Error()})
		}
	} else {
		pvfReady = true
	}
	occ, haveAuction, haveCera, err := a.loadSystemStock()
	if err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{GeneratedAt: time.Now()}
	result.Summary.ExistingRecords = len(occ)
	if needAuction {
		maxActions := req.MaxActions
		if maxActions <= 0 {
			maxActions = a.cfg.Restock.MaxActions
		}
		rows, queueErr := a.nextAuctionQueueRows(pvfReady, catalog, haveAuction, maxActions)
		if queueErr != nil {
			return PlanResult{}, queueErr
		}
		a.planAuction(rows, catalog, haveAuction, occ, &result)
	}
	if needCera {
		if !catalogLoaded {
			var err error
			catalog, err = a.loadCatalog()
			pvfReady = err == nil
			if !pvfReady {
				a.appendLog(LogEvent{Type: "pvf_catalog", Status: "fallback", Message: err.Error()})
			}
		}
		ceraRows := a.cfg.Cera.Items
		a.planCera(ceraRows, nil, haveCera, occ, &result)
	}
	result.Actions = limitActions(result.Actions, req.MaxActions)
	result.Summary = PlanSummary{Actions: len(result.Actions), Skipped: len(result.Skipped), ExistingRecords: result.Summary.ExistingRecords}
	for _, action := range result.Actions {
		if action.Market == "auction" {
			result.Summary.AuctionActions++
		}
		if action.Market == "cera" {
			result.Summary.CeraActions++
		}
	}
	for _, skipped := range result.Skipped {
		switch skipped.Reason {
		case "missing_from_pvf":
			result.Summary.Missing++
		case "risky_special_type":
			result.Summary.Risky++
		case "not_auctionable":
			result.Summary.NotAuctionable++
		}
	}
	a.appendLog(LogEvent{Type: "plan_preview", Market: market, Summary: &result.Summary})
	return result, nil
}

func actionCapacity(maxActions, current int) int {
	if maxActions <= 0 {
		return -1
	}
	if current >= maxActions {
		return 0
	}
	return maxActions - current
}

func limitActions(actions []Action, maxActions int) []Action {
	if maxActions > 0 && len(actions) > maxActions {
		return actions[:maxActions]
	}
	return actions
}

func (a *App) RestockOnce(req RestockRequest) (JobSummary, error) {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if req.MaxActions <= 0 {
		req.MaxActions = a.cfg.Restock.MaxActions
	}
	start := time.Now()
	job := JobSummary{
		ID:        fmt.Sprintf("restock-%d", start.UnixNano()),
		Kind:      "restock",
		Status:    "running",
		StartedAt: start,
	}
	a.appendLog(LogEvent{Type: "job_start", JobID: job.ID, Status: job.Status})
	plan, err := a.Plan(req)
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
		maxActions = a.cfg.Restock.MaxActions
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
	a.appendLog(LogEvent{Type: "job_end", JobID: job.ID, Status: job.Status, Summary: job.Plan})
	return job, nil
}

func (a *App) setLastJob(job JobSummary) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastJob = &job
}

func byteValue(v *byte) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func boolPtr(v bool) *bool {
	return &v
}

func compactJob(job *JobSummary) *JobSummary {
	if job == nil {
		return nil
	}
	out := *job
	out.Actions = nil
	return &out
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
		AutoSyncItemInfo: true,
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
			RefineMin:        1,
			RefineMax:        7,
			UpgradePriceRate: 0.08,
			RefinePriceRate:  0.04,
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
			Enabled:         true,
			Markets:         []string{"auction", "cera"},
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
	itemInfoUnset := !c.AutoSyncItemInfo && c.ItemInfoSourcePath == "" && len(c.ItemInfoTargets) == 0
	if c.ListenAddr == "" {
		c.ListenAddr = d.ListenAddr
	}
	if c.FridaDB == "" {
		c.FridaDB = d.FridaDB
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
	if itemInfoUnset {
		c.AutoSyncItemInfo = d.AutoSyncItemInfo
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
	if c.Restock.RefineMin <= 0 {
		c.Restock.RefineMin = d.Restock.RefineMin
	}
	if c.Restock.RefineMax < c.Restock.RefineMin {
		c.Restock.RefineMax = c.Restock.RefineMin
	}
	if c.Restock.UpgradePriceRate <= 0 {
		c.Restock.UpgradePriceRate = d.Restock.UpgradePriceRate
	}
	if c.Restock.RefinePriceRate <= 0 {
		c.Restock.RefinePriceRate = d.Restock.RefinePriceRate
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
	RefineMin        int               `json:"refine_min"`
	RefineMax        int               `json:"refine_max"`
	UpgradePriceRate float64           `json:"upgrade_price_rate"`
	RefinePriceRate  float64           `json:"refine_price_rate"`
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
	ConfigPath  string             `json:"config_path"`
	LogPath     string             `json:"log_path"`
	ListenAddr  string             `json:"listen_addr"`
	Auto        AutoCfg            `json:"auto"`
	Collector   CollectorCfg       `json:"collector"`
	Restock     RestockCfg         `json:"restock"`
	AutoRunning bool               `json:"auto_running"`
	Ready       bool               `json:"ready"`
	DBInit      []string           `json:"db_init,omitempty"`
	DBInitError string             `json:"db_init_error,omitempty"`
	ItemInfo    ItemInfoSyncStatus `json:"iteminfo"`
	LastJob     *JobSummary        `json:"last_job,omitempty"`
	LogTail     []LogEvent         `json:"log_tail,omitempty"`
}

type ItemInfoSyncStatus struct {
	SourcePath string   `json:"source_path,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	Synced     int      `json:"synced"`
	Skipped    int      `json:"skipped"`
	Error      string   `json:"error,omitempty"`
}

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
	NotAuctionable  int `json:"not_auctionable"`
	ExistingRecords int `json:"existing_records"`
}

type Action struct {
	Market       string `json:"market"`
	Kind         string `json:"kind"`
	Operation    string `json:"operation,omitempty"`
	ItemID       uint32 `json:"item_id"`
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
	Time      time.Time    `json:"time"`
	Type      string       `json:"type"`
	JobID     string       `json:"job_id,omitempty"`
	Status    string       `json:"status,omitempty"`
	Market    string       `json:"market,omitempty"`
	ItemID    uint32       `json:"item_id,omitempty"`
	AuctionID uint64       `json:"auction_id,omitempty"`
	OK        *bool        `json:"ok,omitempty"`
	Reason    interface{}  `json:"reason,omitempty"`
	Message   string       `json:"message,omitempty"`
	Summary   *PlanSummary `json:"summary,omitempty"`
}

// ---- errors.go ----
var ErrExecutorUnavailable = errors.New("market action executor unavailable")

// ---- log.go ----
const marketLogFile = "market_log.jsonl"

func marketLogPath(configDir string) string {
	return filepath.Join(configDir, marketLogFile)
}

func (a *App) appendLog(event LogEvent) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	path := marketLogPath(a.configDir)
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
