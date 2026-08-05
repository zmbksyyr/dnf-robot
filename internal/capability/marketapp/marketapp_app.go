package marketapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"robot/internal/foundation/config"
	"robot/internal/foundation/filewatch"
	"robot/internal/foundation/layout"
	"robot/internal/foundation/lockhub"
	"robot/internal/foundation/logfile"
)

type App struct {
	repository       Repository
	cfg              Config
	configPath       string
	configDir        string
	pvfPath          string
	dfGameR          string
	serviceRoot      string
	serviceRunScript string
	serviceHomeRoot  string
	executors        ActionExecutorFactory
	restarter        func(name, reason string)

	stateMu         lockhub.RWLocker
	jobMu           lockhub.Locker
	autoMu          lockhub.Locker
	logMu           lockhub.Locker
	serviceMu       lockhub.Locker
	serviceSpecMu   lockhub.Locker
	serviceStatusMu lockhub.Locker
	randMu          lockhub.Locker
	addInfoMu       lockhub.Locker
	patchMu         lockhub.Locker
	autoRun         bool
	autoStop        bool
	lastJob         *JobSummary
	dbInit          []string
	dbInitErr       string
	dbInitOK        bool
	dbRetryAt       time.Time
	dbGeneration    uint64
	itemInfo        ItemInfoSyncStatus
	services        map[string]MarketServiceStatus
	serviceSpecs    []marketServiceSpec
	rand            *rand.Rand
	autoDone        chan struct{}
	autoCtx         context.Context
	autoCancel      context.CancelFunc
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	autoRestart     bool
	autoShutdown    bool

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
	logMaxBytes         int64
	logBackups          int
	logWriter           *logfile.Appender
	logClosed           bool
	priceRanges         map[uint32]customPriceRange
	priceRangeStatus    PriceRangeStatus
	runtimeFilesWatched atomic.Bool
	rebuildRunning      atomic.Bool
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

type itemInfoEntry struct {
	ItemType int
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

type Repository interface {
	EnsureMarketTables(dbNames []string, now time.Time) ([]string, error)
	LoadCollectRows(dbName, market string, systemOwnerBase uint32, includeSystemOwners bool) ([]collectRow, error)
	LoadSystemCollectRows(dbName, market string, systemOwnerBase uint32) ([]collectRow, error)
	LoadMarketStock(dbName string, systemOwnerBase uint32, occupied map[uint32]int) (map[uint32]int, error)
	CountSystemStockKinds(dbName string, systemOwnerBase uint32) (int, error)
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

func New(db *sql.DB, sys *config.SysConfig, executors ActionExecutorFactory) (*App, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	if sys == nil {
		return nil, errors.New("nil system config")
	}
	if strings.TrimSpace(sys.ConfigDir) == "" {
		return nil, errors.New("empty config dir")
	}
	if executors == nil {
		executors = unsupportedActionExecutorFactory{}
	}
	logMaxSizeMB := sys.LogMaxSizeMB
	if logMaxSizeMB <= 0 {
		logMaxSizeMB = defaultMarketLogMaxSizeMB
	}
	logBackups := sys.LogMaxBackups
	if logBackups <= 0 {
		logBackups = defaultMarketLogBackups
	}
	cfg, path, err := loadConfig(sys.ConfigDir)
	if err != nil {
		return nil, err
	}
	cfg.AuctionHost = strings.TrimSpace(sys.AuctionHost)
	if cfg.AuctionHost == "" {
		cfg.AuctionHost = "127.0.0.1"
	}
	if sys.AuctionPort > 0 {
		cfg.AuctionPort = sys.AuctionPort
	}
	cfg.CeraHost = strings.TrimSpace(sys.PointHost)
	if cfg.CeraHost == "" {
		cfg.CeraHost = "127.0.0.1"
	}
	if sys.PointPort > 0 {
		cfg.CeraPort = sys.PointPort
	}
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	app := &App{
		repository:         SQLRepository{db: db},
		cfg:                cfg,
		configPath:         path,
		configDir:          sys.ConfigDir,
		pvfPath:            filepath.Join(filepath.Dir(sys.DFGameR), "Script.pvf"),
		dfGameR:            sys.DFGameR,
		serviceRoot:        strings.TrimSpace(sys.ServiceRoot),
		serviceRunScript:   sys.ServiceRunScript,
		executors:          executors,
		services:           map[string]MarketServiceStatus{},
		policy:             map[string]MarketPolicyStatus{},
		lastServiceRestart: map[string]time.Time{},
		priceRanges:        map[uint32]customPriceRange{},
		logMaxBytes:        int64(logMaxSizeMB) * 1024 * 1024,
		logBackups:         logBackups,
		rand:               rand.New(rand.NewSource(time.Now().UnixNano())),
		autoDone:           make(chan struct{}),
		lifecycleCtx:       lifecycleCtx,
		lifecycleCancel:    lifecycleCancel,
	}
	app.setConfig(cfg)
	app.refreshCustomPriceRanges()
	app.itemInfo = app.ensureConfiguredCeraItemInfo()
	app.ensureMarketTables(time.Now())
	app.refreshMarketServiceStatuses()
	return app, nil
}

func (a *App) RuntimeFileEntries() []filewatch.Entry {
	if a == nil {
		return nil
	}
	paths := layout.New(a.configDir)
	a.runtimeFilesWatched.Store(true)
	return []filewatch.Entry{
		{Name: "market_config", Path: paths.MarketConfig(), Apply: a.reloadMarketConfigFile},
		{Name: "market_prices", Path: paths.MarketPrices(), Apply: a.reloadCustomPriceRangeFile},
	}
}

func (a *App) reloadMarketConfigFile(path string) error {
	cfg, err := loadConfigSnapshot(path)
	if err != nil {
		return err
	}

	a.jobMu.Lock()
	previous := a.configSnapshot()
	cfg.AuctionHost, cfg.AuctionPort = previous.AuctionHost, previous.AuctionPort
	cfg.CeraHost, cfg.CeraPort = previous.CeraHost, previous.CeraPort
	if reflect.DeepEqual(previous, cfg) {
		a.jobMu.Unlock()
		a.reconcileAutoRuntime(cfg.Auto, cfg.Auto)
		return nil
	}
	databaseChanged := previous.GameDB != cfg.GameDB || previous.AuctionDB != cfg.AuctionDB || previous.CeraDB != cfg.CeraDB
	itemInfoChanged := !reflect.DeepEqual(previous.ItemInfoTargets, cfg.ItemInfoTargets) || !reflect.DeepEqual(previous.Cera.Items, cfg.Cera.Items)
	a.setConfig(cfg)
	if databaseChanged {
		a.ensureMarketTables(time.Now())
	}
	if itemInfoChanged {
		status := a.ensureConfiguredCeraItemInfo()
		a.stateMu.Lock()
		a.itemInfo = status
		a.stateMu.Unlock()
	}
	a.jobMu.Unlock()

	if err := a.reloadCustomPriceRangeFile(a.customPriceRangePath()); err != nil {
		a.appendLog(LogEvent{Type: "config", Status: marketLogStatusFallback, Message: "market price snapshot retained: " + err.Error()})
	}
	a.reconcileAutoRuntime(previous.Auto, cfg.Auto)
	a.appendLog(LogEvent{Type: "config", Status: marketLogStatusSuccess, Message: "market runtime config reloaded"})
	return nil
}

func (a *App) reconcileAutoRuntime(previous, current AutoCfg) {
	if !current.Enabled {
		a.StopAutoAsync()
		return
	}
	if !previous.Enabled {
		a.RestartAutoAsync()
		return
	}
	if !a.AutoRunning() {
		a.StartAuto()
		return
	}
	if marketAutoScheduleChanged(previous, current) {
		a.RestartAutoAsync()
	}
}

func marketAutoScheduleChanged(old, current AutoCfg) bool {
	return old.InitialDelayMS != current.InitialDelayMS ||
		old.IntervalMS != current.IntervalMS ||
		!reflect.DeepEqual(old.Markets, current.Markets)
}

func (a *App) Config() Config {
	return cloneConfig(a.configSnapshot())
}

func (a *App) Status() Status {
	a.refreshMarketServiceStatuses()
	cfg := a.Config()
	a.stateMu.RLock()
	dbInit := append([]string(nil), a.dbInit...)
	dbInitErr := a.dbInitErr
	itemInfo := cloneItemInfoStatus(a.itemInfo)
	services := cloneServiceStatusMap(a.services)
	policy := clonePolicyStatusMap(a.policy)
	lastJob := compactJob(a.lastJob)
	priceRanges := a.priceRangeStatus
	a.stateMu.RUnlock()
	return Status{
		ConfigPath:  a.configPath,
		LogPath:     marketLogPath(a.configDir),
		Auto:        cfg.Auto,
		Collector:   cfg.Collector,
		Restock:     cfg.Restock,
		PriceRanges: priceRanges,
		AutoRunning: a.AutoRunning(),
		Ready:       true,
		DBInit:      dbInit,
		DBInitError: dbInitErr,
		ItemInfo:    itemInfo,
		Services:    services,
		Policy:      policy,
		LastJob:     lastJob,
	}
}

func (a *App) EnsureServices(markets []string) (Status, error) {
	for i, market := range markets {
		normalized, err := ValidateExternalMarketName(market)
		if err != nil {
			return a.Status(), fmt.Errorf("ensure services: %w", err)
		}
		markets[i] = normalized
	}
	if a.dfGameRRunning() {
		a.ensureMarketServices(markets)
	} else {
		a.refreshMarketServiceStatuses()
	}
	return a.Status(), nil
}

func (a *App) SetAutoEnabled(enabled bool) (Status, error) {
	a.jobMu.Lock()
	cfg := cloneConfig(a.configSnapshot())
	cfg.Auto.Enabled = enabled
	err := writeMarketConfig(a.configPath, cfg)
	if err == nil {
		a.setConfig(cfg)
	}
	a.jobMu.Unlock()
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
	a.jobMu.Lock()
	previous := a.configSnapshot()
	cfg := cloneConfig(previous)
	if req.AutoEnabled != nil {
		cfg.Auto.Enabled = *req.AutoEnabled
	}
	if req.CollectorEnabled != nil {
		cfg.Collector.Enabled = *req.CollectorEnabled
	}
	if req.EquipmentAllowedRarities != nil {
		allowed, err := normalizeAllowedRarities(*req.EquipmentAllowedRarities)
		if err != nil {
			a.jobMu.Unlock()
			return Status{}, err
		}
		cfg.Restock.EquipmentAllowedRarities = allowed
	}
	if req.OtherAllowedRarities != nil {
		allowed, err := normalizeAllowedRarities(*req.OtherAllowedRarities)
		if err != nil {
			a.jobMu.Unlock()
			return Status{}, err
		}
		cfg.Restock.OtherAllowedRarities = allowed
	}
	if req.EquipmentTradePolicy != nil {
		cfg.Restock.EquipmentTradePolicy = strings.TrimSpace(*req.EquipmentTradePolicy)
	}
	if req.OtherTradePolicy != nil {
		cfg.Restock.OtherTradePolicy = strings.TrimSpace(*req.OtherTradePolicy)
	}
	if req.IntervalMS != nil {
		cfg.Auto.IntervalMS = *req.IntervalMS
	}
	if req.InitialDelayMS != nil {
		cfg.Auto.InitialDelayMS = *req.InitialDelayMS
	}
	if req.AutoMaxActions != nil {
		cfg.Auto.MaxActions = *req.AutoMaxActions
	}
	if req.AutoMaxConcurrent != nil {
		cfg.Auto.MaxConcurrent = *req.AutoMaxConcurrent
	}
	if req.RestockMaxActions != nil {
		cfg.Restock.MaxActions = *req.RestockMaxActions
	}
	if req.RestockMaxConcurrent != nil {
		cfg.Restock.MaxConcurrent = *req.RestockMaxConcurrent
	}
	if req.CollectorMaxActions != nil {
		cfg.Collector.MaxActions = *req.CollectorMaxActions
	}
	if req.CollectorMaxConcurrent != nil {
		cfg.Collector.MaxConcurrent = *req.CollectorMaxConcurrent
	}
	if req.ContinueOnError != nil {
		cfg.Auto.ContinueOnError = *req.ContinueOnError
	}
	if req.Markets != nil {
		cfg.Auto.Markets = append([]string(nil), req.Markets...)
	}
	if req.StackSizes != nil {
		cfg.Restock.StackSizes = append([]int(nil), req.StackSizes...)
	}
	if req.EquipmentQtyMin != nil {
		cfg.Restock.EquipmentQtyMin = *req.EquipmentQtyMin
	}
	if req.EquipmentQtyMax != nil {
		cfg.Restock.EquipmentQtyMax = *req.EquipmentQtyMax
	}
	if req.EquipInflateMin != nil {
		cfg.Restock.EquipInflateMin = *req.EquipInflateMin
	}
	if req.EquipInflateMax != nil {
		cfg.Restock.EquipInflateMax = *req.EquipInflateMax
	}
	if req.EquipmentLevelMin != nil {
		cfg.Restock.EquipmentLevelMin = *req.EquipmentLevelMin
	}
	if req.EquipmentLevelMax != nil {
		cfg.Restock.EquipmentLevelMax = *req.EquipmentLevelMax
	}
	if req.UpgradeMin != nil {
		cfg.Restock.UpgradeMin = *req.UpgradeMin
	}
	if req.UpgradeMax != nil {
		cfg.Restock.UpgradeMax = *req.UpgradeMax
	}
	if req.UpgradePriceRate != nil {
		cfg.Restock.UpgradePriceRate = *req.UpgradePriceRate
	}
	if req.RandLow != nil {
		cfg.Restock.RandLow = *req.RandLow
	}
	if req.RandHigh != nil {
		cfg.Restock.RandHigh = *req.RandHigh
	}
	if req.CustomPriceEnabled != nil {
		cfg.Restock.CustomPriceEnabled = *req.CustomPriceEnabled
	}
	if req.PriceRangeEnabled != nil {
		cfg.Collector.PriceRangeEnabled = *req.PriceRangeEnabled
	}
	if req.InRangeProbability != nil {
		cfg.Collector.InRangeProbability = *req.InRangeProbability
	}
	if req.OutRangeProbability != nil {
		cfg.Collector.OutRangeProbability = *req.OutRangeProbability
	}
	if req.RestockPerItemDelayMS != nil {
		cfg.Restock.PerItemDelayMS = *req.RestockPerItemDelayMS
	}
	err := writeMarketConfig(a.configPath, cfg)
	if err == nil {
		a.setConfig(cfg)
	}
	a.jobMu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	if a.runtimeFilesWatched.Load() {
		if priceErr := a.reloadCustomPriceRangeFile(a.customPriceRangePath()); priceErr != nil {
			a.appendLog(LogEvent{Type: "config", Status: marketLogStatusFallback, Message: "market price snapshot retained: " + priceErr.Error()})
		}
	} else {
		a.refreshCustomPriceRanges()
	}
	a.reconcileAutoRuntime(previous.Auto, cfg.Auto)
	return a.Status(), nil
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

func cloneItemInfoStatus(in ItemInfoSyncStatus) ItemInfoSyncStatus {
	in.Targets = append([]string(nil), in.Targets...)
	return in
}
