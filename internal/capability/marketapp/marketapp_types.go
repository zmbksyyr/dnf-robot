package marketapp

import "time"

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
	QualityFilter    *bool             `json:"quality_filter,omitempty"`
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
	Market          string   `json:"market,omitempty"`
	Execute         bool     `json:"execute,omitempty"`
	MaxActions      int      `json:"max_actions,omitempty"`
	MaxConcurrent   int      `json:"max_concurrent,omitempty"`
	ContinueOnError bool     `json:"continue_on_error,omitempty"`
	ItemIDs         []uint32 `json:"item_ids,omitempty"`
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
	QualityFilter    *bool    `json:"quality_filter,omitempty"`
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
	Endurance    int    `json:"endurance,omitempty"`
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
