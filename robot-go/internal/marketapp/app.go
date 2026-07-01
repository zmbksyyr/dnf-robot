package marketapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"robot/internal/config"
	"robot/internal/service"
)

type App struct {
	db         *sql.DB
	cfg        Config
	configPath string
	configDir  string
	pvfPath    string
	rs         *service.RobotSvc

	mu        sync.Mutex
	jobMu     sync.Mutex
	autoMu    sync.Mutex
	autoRun   bool
	lastJob   *JobSummary
	dbInit    []string
	dbInitErr string
	itemInfo  ItemInfoSyncStatus
	rand      *rand.Rand
	stopAuto  chan struct{}
	autoDone  chan struct{}
}

func New(db *sql.DB, sys *config.SysConfig) (*App, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	if sys == nil {
		return nil, errors.New("nil system config")
	}
	cfg, path, err := LoadConfig(sys.ConfigDir)
	if err != nil {
		return nil, err
	}
	if err := ensureRestockFile(sys.ConfigDir, cfg); err != nil {
		return nil, err
	}
	app := &App{
		db:         db,
		cfg:        cfg,
		configPath: path,
		configDir:  sys.ConfigDir,
		pvfPath:    filepath.Join(filepath.Dir(sys.DFGameR), "Script.pvf"),
		rs:         &service.RobotSvc{},
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
		stopAuto:   make(chan struct{}),
		autoDone:   make(chan struct{}),
	}
	app.itemInfo = app.itemInfoStatus()
	if tables, err := app.ensureMarketTables(time.Now()); err != nil {
		app.dbInit = tables
		app.dbInitErr = err.Error()
		app.appendLog(LogEvent{Type: "db_init", Status: "failed", Message: err.Error()})
	} else {
		app.dbInit = tables
		app.appendLog(LogEvent{Type: "db_init", Status: "success", Message: strings.Join(tables, ",")})
	}
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
		RestockPath: restockFilePath(a.configDir, a.cfg),
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
	catalog, err := a.loadCatalog()
	if err != nil {
		return PlanResult{}, err
	}
	seed, err := a.loadRestockSeed()
	if err != nil {
		return PlanResult{}, err
	}
	occ, haveAuction, haveCera, err := a.loadSystemStock()
	if err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{GeneratedAt: time.Now()}
	result.Summary.ExistingRecords = len(occ)
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" || market == "auction" {
		a.planAuction(seed.Auction, catalog, haveAuction, occ, &result)
	}
	if market == "" || market == "cera" || market == "gold" {
		a.planCera(seed.Cera, catalog, haveCera, occ, &result)
	}
	sort.Slice(result.Actions, func(i, j int) bool {
		if result.Actions[i].Market == result.Actions[j].Market {
			return result.Actions[i].ItemID < result.Actions[j].ItemID
		}
		return result.Actions[i].Market < result.Actions[j].Market
	})
	result.Summary.Actions = len(result.Actions)
	result.Summary.Skipped = len(result.Skipped)
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
	if req.MaxActions > 0 && len(result.Actions) > req.MaxActions {
		result.Actions = result.Actions[:req.MaxActions]
	}
	a.appendLog(LogEvent{Type: "plan_preview", Market: market, Summary: &result.Summary})
	return result, nil
}

func (a *App) RestockOnce(req RestockRequest) (JobSummary, error) {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
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
	failedActions, firstErr := a.executeActions(job.ID, actions, req.MaxConcurrent, req.ContinueOnError, &job)
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
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, ItemType: item.ItemType, Rarity: item.Rarity, StackLimit: item.StackLimit}
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
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, ItemType: item.ItemType, Rarity: item.Rarity, StackLimit: item.StackLimit}
	}
	a.overlayItemInfoDAT(out, a.cfg.CeraItemInfoPath)
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

func (a *App) overlayItemInfoDAT(items map[uint32]catalogItem, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		id64, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil || id64 == 0 {
			continue
		}
		id := uint32(id64)
		if _, ok := items[id]; !ok {
			items[id] = catalogItem{ItemID: id, Kind: "stackable"}
		}
	}
}
