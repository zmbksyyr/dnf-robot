package marketapp

import (
	"database/sql"
	"errors"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
)

type App struct {
	repository       Repository
	cfg              Config
	configPath       string
	configDir        string
	pvfPath          string
	dfGameR          string
	serviceRunScript string
	serviceHomeRoot  string
	executors        ActionExecutorFactory
	restarter        func(name, reason string)

	stateMu   lockhub.Locker
	jobMu     lockhub.Locker
	autoMu    lockhub.Locker
	logMu     lockhub.Locker
	serviceMu lockhub.Locker
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
	logMaxBytes         int64
	logBackups          int
	priceRanges         map[uint32]customPriceRange
	priceRangeStatus    PriceRangeStatus
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
	cfg, path, loadStatus, err := loadConfig(sys.ConfigDir)
	if err != nil {
		return nil, err
	}
	cfg.AuctionHost = "127.0.0.1"
	if sys.AuctionPort > 0 {
		cfg.AuctionPort = sys.AuctionPort
	}
	cfg.CeraHost = "127.0.0.1"
	if sys.PointPort > 0 {
		cfg.CeraPort = sys.PointPort
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
		priceRanges:        map[uint32]customPriceRange{},
		logMaxBytes:        int64(logMaxSizeMB) * 1024 * 1024,
		logBackups:         logBackups,
		rand:               rand.New(rand.NewSource(time.Now().UnixNano())),
		stopAuto:           make(chan struct{}),
		autoDone:           make(chan struct{}),
	}
	app.refreshCustomPriceRanges()
	app.itemInfo = app.ensureConfiguredCeraItemInfo()
	if loadStatus.Recovered {
		app.appendLog(LogEvent{
			Type:    "config",
			Status:  marketLogStatusFallback,
			Message: "invalid legacy market config backed up to " + loadStatus.BackupPath + ": " + loadStatus.Reason,
		})
	}
	if loadStatus.MigratedFrom != "" {
		app.appendLog(LogEvent{Type: "config", Status: marketLogStatusSuccess, Message: "migrated legacy market config from " + loadStatus.MigratedFrom + " to " + path})
	}
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
		Auto:        a.cfg.Auto,
		Collector:   a.cfg.Collector,
		Restock:     a.cfg.Restock,
		PriceRanges: a.priceRangeStatus,
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

func (a *App) EnsureServices(markets []string) Status {
	if a.dfGameRRunning() {
		a.ensureMarketServices(markets)
	} else {
		a.refreshMarketServiceStatuses()
	}
	return a.Status()
}

func (a *App) SetAutoEnabled(enabled bool) (Status, error) {
	a.stateMu.Lock()
	a.cfg.Auto.Enabled = enabled
	err := writeMarketConfig(a.configPath, a.cfg)
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
	if req.QualityFilter != nil {
		a.cfg.Restock.QualityFilter = req.QualityFilter
	}
	if req.IntervalMS > 0 {
		a.cfg.Auto.IntervalMS = req.IntervalMS
	}
	if req.InitialDelayMS != nil && *req.InitialDelayMS >= 0 {
		a.cfg.Auto.InitialDelayMS = *req.InitialDelayMS
	}
	if req.MaxActions != nil && *req.MaxActions >= 0 {
		a.cfg.Auto.MaxActions = *req.MaxActions
		a.cfg.Collector.MaxActions = *req.MaxActions
		a.cfg.Restock.MaxActions = *req.MaxActions
	}
	if req.MaxConcurrent != nil && *req.MaxConcurrent > 0 {
		a.cfg.Auto.MaxConcurrent = *req.MaxConcurrent
		a.cfg.Collector.MaxConcurrent = *req.MaxConcurrent
		a.cfg.Restock.MaxConcurrent = *req.MaxConcurrent
	}
	if req.AutoMaxActions != nil && *req.AutoMaxActions >= 0 {
		a.cfg.Auto.MaxActions = *req.AutoMaxActions
	}
	if req.AutoMaxConcurrent != nil && *req.AutoMaxConcurrent > 0 {
		a.cfg.Auto.MaxConcurrent = *req.AutoMaxConcurrent
	}
	if req.RestockMaxActions != nil && *req.RestockMaxActions >= 0 {
		a.cfg.Restock.MaxActions = *req.RestockMaxActions
	}
	if req.RestockMaxConcurrent != nil && *req.RestockMaxConcurrent > 0 {
		a.cfg.Restock.MaxConcurrent = *req.RestockMaxConcurrent
	}
	if req.CollectorMaxActions != nil && *req.CollectorMaxActions >= 0 {
		a.cfg.Collector.MaxActions = *req.CollectorMaxActions
	}
	if req.CollectorMaxConcurrent != nil && *req.CollectorMaxConcurrent > 0 {
		a.cfg.Collector.MaxConcurrent = *req.CollectorMaxConcurrent
	}
	if req.ContinueOnError != nil {
		a.cfg.Auto.ContinueOnError = *req.ContinueOnError
	}
	if len(req.Markets) > 0 {
		a.cfg.Auto.Markets = req.Markets
	}
	if len(req.StackSizes) > 0 {
		a.cfg.Restock.StackSizes = append([]int(nil), req.StackSizes...)
	}
	if req.EquipmentQtyMin != nil {
		a.cfg.Restock.EquipmentQtyMin = *req.EquipmentQtyMin
	}
	if req.EquipmentQtyMax != nil {
		a.cfg.Restock.EquipmentQtyMax = *req.EquipmentQtyMax
	}
	if req.EquipInflateMin != nil {
		a.cfg.Restock.EquipInflateMin = *req.EquipInflateMin
	}
	if req.EquipInflateMax != nil {
		a.cfg.Restock.EquipInflateMax = *req.EquipInflateMax
	}
	if req.EquipmentLevelMin != nil {
		a.cfg.Restock.EquipmentLevelMin = *req.EquipmentLevelMin
	}
	if req.EquipmentLevelMax != nil {
		a.cfg.Restock.EquipmentLevelMax = *req.EquipmentLevelMax
	}
	if req.UpgradeMin != nil {
		a.cfg.Restock.UpgradeMin = *req.UpgradeMin
	}
	if req.UpgradeMax != nil {
		a.cfg.Restock.UpgradeMax = *req.UpgradeMax
	}
	if req.UpgradePriceRate != nil {
		a.cfg.Restock.UpgradePriceRate = *req.UpgradePriceRate
	}
	if req.RandLow != nil {
		a.cfg.Restock.RandLow = *req.RandLow
	}
	if req.RandHigh != nil {
		a.cfg.Restock.RandHigh = *req.RandHigh
	}
	if req.CustomPriceEnabled != nil {
		a.cfg.Restock.CustomPriceEnabled = *req.CustomPriceEnabled
	}
	if req.CustomPriceFile != nil {
		a.cfg.Restock.CustomPriceFile = strings.TrimSpace(*req.CustomPriceFile)
	}
	if req.PriceRangeEnabled != nil {
		a.cfg.Collector.PriceRangeEnabled = *req.PriceRangeEnabled
	}
	if req.InRangeProbability != nil {
		a.cfg.Collector.InRangeProbability = *req.InRangeProbability
	}
	if req.OutRangeProbability != nil {
		a.cfg.Collector.OutRangeProbability = *req.OutRangeProbability
	}
	if req.RestockPerItemDelayMS != nil {
		a.cfg.Restock.PerItemDelayMS = *req.RestockPerItemDelayMS
	}
	a.cfg.applyDefaults()
	cfg := a.cfg
	err := writeMarketConfig(a.configPath, cfg)
	a.stateMu.Unlock()
	if err != nil {
		return a.Status(), err
	}
	a.refreshCustomPriceRanges()
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
