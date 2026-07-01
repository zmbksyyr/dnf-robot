package marketapp

import "time"

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
	CeraItemInfoPath   string       `json:"cera_iteminfo_path"`
	SystemOwner        SystemOwner  `json:"system_owner"`
	Collector          CollectorCfg `json:"collector"`
	Restock            RestockCfg   `json:"restock"`
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
	File             string  `json:"file"`
	RandLow          float64 `json:"rand_low"`
	RandHigh         float64 `json:"rand_high"`
	MaxActions       int     `json:"max_actions"`
	MaxConcurrent    int     `json:"max_concurrent"`
	MaxResultActions int     `json:"max_result_actions"`
	PerItemDelayMS   int     `json:"per_item_delay_ms"`
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

type PVFUpgradeSeparateRequest struct {
	Path   string `json:"path,omitempty"`
	Target int    `json:"target,omitempty"`
}

type ConfigUpdateRequest struct {
	AutoEnabled      *bool    `json:"auto_enabled,omitempty"`
	CollectorEnabled *bool    `json:"collector_enabled,omitempty"`
	IntervalMS       int      `json:"interval_ms,omitempty"`
	InitialDelayMS   int      `json:"initial_delay_ms,omitempty"`
	MaxActions       int      `json:"max_actions,omitempty"`
	MaxConcurrent    int      `json:"max_concurrent,omitempty"`
	ContinueOnError  *bool    `json:"continue_on_error,omitempty"`
	Markets          []string `json:"markets,omitempty"`
}

type Status struct {
	ConfigPath  string             `json:"config_path"`
	RestockPath string             `json:"restock_path"`
	LogPath     string             `json:"log_path"`
	ListenAddr  string             `json:"listen_addr"`
	Auto        AutoCfg            `json:"auto"`
	Collector   CollectorCfg       `json:"collector"`
	Restock     RestockCfg         `json:"restock"`
	AutoRunning bool               `json:"auto_running"`
	Ready       bool               `json:"ready"`
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
	ItemType   int    `json:"item_type"`
	Slot       string `json:"slot,omitempty"`
	Rarity     int    `json:"rarity,omitempty"`
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
	ItemType   int
	Rarity     int
	StackLimit int
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
}

type ceraRow struct {
	ItemID       uint32 `json:"item_id"`
	Label        string `json:"name"`
	RestockPrice int32  `json:"restock_price"`
	RestockQty   int    `json:"restock_qty"`
	RecyclePrice int32  `json:"recycle_price"`
	Enabled      bool   `json:"enabled"`
}

type restockSeed struct {
	Version int          `json:"version"`
	Auction []restockRow `json:"auction"`
	Cera    []ceraRow    `json:"cera"`
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
