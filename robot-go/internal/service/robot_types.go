package service

import "time"

type RobotCreateRequest struct {
	Count int `json:"count"`
}

type RobotInfo struct {
	UID     int    `json:"uid"`
	CID     int    `json:"cid"`
	Name    string `json:"name"`
	Level   int    `json:"level"`
	Job     int    `json:"job"`
	Grow    int    `json:"grow_type"`
	Port    int    `json:"port"`
	Village int    `json:"village"`
	Area    int    `json:"area"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
}

type storePosition struct {
	Village int
	Area    int
	X       int
	Y       int
	Source  string
	PointID string
	Region  string
}

type followTarget struct {
	UID     int
	Village int
	Area    int
	X       int
	Y       int
}

type RobotCommandRequest struct {
	Count int   `json:"count"`
	UIDs  []int `json:"uids"`
}

type RobotCleanupRequest struct {
	UIDs                    []int `json:"uids"`
	MinUID                  int   `json:"uid_min"`
	MaxUID                  int   `json:"uid_max"`
	Force                   bool  `json:"force"`
	InternalConfirmedBroken bool  `json:"-"`
}

type RobotActionResult struct {
	UID     int    `json:"uid"`
	CID     int    `json:"cid,omitempty"`
	OK      bool   `json:"ok"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type RobotCommandResult struct {
	Requested int                 `json:"requested"`
	Accepted  int                 `json:"accepted"`
	Confirmed int                 `json:"confirmed"`
	Failed    int                 `json:"failed"`
	Robots    []RobotActionResult `json:"robots"`
}

type RobotCleanupCandidate struct {
	UID       int    `json:"uid"`
	CID       int    `json:"cid"`
	Name      string `json:"name"`
	Account   string `json:"account"`
	Protected bool   `json:"protected"`
	Reason    string `json:"reason,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
}

type RobotCleanupResult struct {
	DryRun     bool                    `json:"dry_run"`
	Requested  int                     `json:"requested"`
	Candidates []RobotCleanupCandidate `json:"candidates"`
	Deleted    int                     `json:"deleted"`
	Skipped    int                     `json:"skipped"`
}

type RobotAutoStatus struct {
	Enabled           bool      `json:"enabled"`
	TargetOnline      int       `json:"target_online"`
	Actors            int       `json:"actors"`
	Leased            int       `json:"leased"`
	Idle              int       `json:"idle"`
	Recycling         int       `json:"recycling"`
	BlockedUIDs       int       `json:"blocked_uids"`
	ActorIdle         int       `json:"actor_idle"`
	ActorAssigned     int       `json:"actor_assigned"`
	ActorOnline       int       `json:"actor_online"`
	ActorRunning      int       `json:"actor_running"`
	ActorBusy         int       `json:"actor_busy"`
	ActorReleasing    int       `json:"actor_releasing"`
	Running           int       `json:"running"`
	Connecting        int       `json:"connecting"`
	GamePortReady     bool      `json:"game_port_ready"`
	GamePortAddress   string    `json:"game_port_address,omitempty"`
	GamePortStableAt  time.Time `json:"game_port_stable_at,omitempty"`
	StoreProbability  int       `json:"store_probability_percent"`
	StoreRunning      int       `json:"store_running"`
	Created           int       `json:"created"`
	OnlineSuccess     int       `json:"online_success"`
	OnlineFailed      int       `json:"online_failed"`
	MoveSuccess       int       `json:"move_success"`
	MoveFailed        int       `json:"move_failed"`
	ShoutLocalSuccess int       `json:"shout_local_success"`
	ShoutLocalFailed  int       `json:"shout_local_failed"`
	ShoutWorldSuccess int       `json:"shout_world_success"`
	ShoutWorldFailed  int       `json:"shout_world_failed"`
	StoreSuccess      int       `json:"store_success"`
	StoreFailed       int       `json:"store_failed"`
	StoreExpired      int       `json:"store_expired"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SchedulerStatus struct {
	Mode                    string    `json:"mode"`
	Reason                  string    `json:"reason"`
	RecentOperation         string    `json:"recent_operation,omitempty"`
	RecentOperationState    string    `json:"recent_operation_state,omitempty"`
	RecentOperationSummary  string    `json:"recent_operation_summary,omitempty"`
	TargetOnline            int       `json:"target_online"`
	Running                 int       `json:"running"`
	Connecting              int       `json:"connecting"`
	Actors                  int       `json:"actors"`
	Idle                    int       `json:"idle"`
	ActorIdle               int       `json:"actor_idle"`
	ActorAssigned           int       `json:"actor_assigned"`
	ActorOnline             int       `json:"actor_online"`
	ActorRunning            int       `json:"actor_running"`
	ActorBusy               int       `json:"actor_busy"`
	ActorReleasing          int       `json:"actor_releasing"`
	StoreRunning            int       `json:"store_running"`
	GamePortReady           bool      `json:"game_port_ready"`
	BreakerActive           bool      `json:"breaker_active"`
	CPUPercent              float64   `json:"cpu_percent"`
	MemoryMB                int       `json:"memory_mb"`
	Goroutines              int       `json:"goroutines"`
	OnlineBatchSize         int       `json:"online_batch_size"`
	OnlineStartRate         int       `json:"online_start_rate"`
	OnlineFillTimeoutSec    int       `json:"online_fill_timeout_sec"`
	MoveIntervalMinSec      int       `json:"move_interval_min_sec"`
	MoveIntervalMaxSec      int       `json:"move_interval_max_sec"`
	ShoutIntervalMinSec     int       `json:"shout_interval_min_sec"`
	ShoutIntervalMaxSec     int       `json:"shout_interval_max_sec"`
	StoreConcurrent         int       `json:"store_concurrent"`
	StoreProbabilityPercent int       `json:"store_probability_percent"`
	StoreIntervalMinSec     int       `json:"store_interval_min_sec"`
	StoreIntervalMaxSec     int       `json:"store_interval_max_sec"`
	StoreDurationSec        int       `json:"store_duration_sec"`
	StoreTickSec            int       `json:"store_tick_sec"`
	StoreMaxPositionTries   int       `json:"store_max_position_tries"`
	StoreFailCooldownSec    int       `json:"store_fail_cooldown_sec"`
	ScaleUpBatch            int       `json:"scale_up_batch"`
	ScaleDownBatch          int       `json:"scale_down_batch"`
	BreakerReleaseBatch     int       `json:"breaker_release_batch"`
	PortDownReleaseBatch    int       `json:"port_down_release_batch"`
	OperationActive         bool      `json:"operation_active"`
	Operation               string    `json:"operation,omitempty"`
	OperationStartedAt      time.Time `json:"operation_started_at,omitempty"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type RobotOperationStatus struct {
	ID         int64     `json:"id"`
	Type       string    `json:"type"`
	Scope      string    `json:"scope,omitempty"`
	State      string    `json:"state"`
	Summary    string    `json:"summary,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type RobotConfigUpdateRequest struct {
	Text    string                 `json:"text"`
	Updates map[string]interface{} `json:"updates"`
}

type RobotConfigResult struct {
	Path   string             `json:"path"`
	Text   string             `json:"text"`
	Config robotRuntimeConfig `json:"config"`
}

type robotRuntimeConfig struct {
	LevelMin                      int    `json:"level_min"`
	LevelMax                      int    `json:"level_max"`
	Jobs                          []int  `json:"jobs"`
	GrowTypes                     []int  `json:"grow_types"`
	RobotUIDStart                 int    `json:"robot_uid_start"`
	NameASCIIFallback             bool   `json:"name_ascii_fallback"`
	NameASCIIPrefix               string `json:"name_ascii_prefix"`
	SpawnFixed                    bool   `json:"spawn_fixed"`
	SpawnVillage                  int    `json:"spawn_village"`
	SpawnFallbackVillage          int    `json:"spawn_fallback_village"`
	SpawnArea                     int    `json:"spawn_area"`
	SpawnXMin                     int    `json:"spawn_x_min"`
	SpawnXMax                     int    `json:"spawn_x_max"`
	SpawnYMin                     int    `json:"spawn_y_min"`
	SpawnYMax                     int    `json:"spawn_y_max"`
	MoveSpeedMin                  int    `json:"move_speed_min"`
	MoveSpeedMax                  int    `json:"move_speed_max"`
	MoveType                      int    `json:"move_type"`
	MoveSteps                     int    `json:"move_steps"`
	MoveStepDelayMS               int    `json:"move_step_delay_ms"`
	LoginDelayMS                  int    `json:"login_delay_ms"`
	ReconnectDelayMS              int    `json:"reconnect_delay_ms"`
	MaxReconnect                  int    `json:"max_reconnect"`
	MaxOnlineRobots               int    `json:"max_online_robots"`
	MaxOnlinePerCommand           int    `json:"max_online_per_command"`
	OnlineDispatchIntervalMS      int    `json:"online_dispatch_interval_ms"`
	OnlineConfirmTimeoutMS        int    `json:"online_confirm_timeout_ms"`
	DefaultMoney                  int    `json:"default_money"`
	DefaultCoin                   int    `json:"default_coin"`
	InventoryCapacity             int    `json:"inventory_capacity"`
	EquipSlots                    []int  `json:"equip_slots"`
	EquipRarityMin                int    `json:"equip_rarity_min"`
	EquipRarityMax                int    `json:"equip_rarity_max"`
	EquipIntensifyMin             int    `json:"equip_intensify_min"`
	EquipIntensifyMax             int    `json:"equip_intensify_max"`
	EquipSmithingMin              int    `json:"equip_smithing_min"`
	EquipSmithingMax              int    `json:"equip_smithing_max"`
	PreferEquipSets               bool   `json:"prefer_equip_sets"`
	EquipSetMinSlots              int    `json:"equip_set_min_slots"`
	AvatarSlots                   []int  `json:"avatar_slots"`
	MinAvatarSlots                int    `json:"min_avatar_slots"`
	PreferAvatarSets              bool   `json:"prefer_avatar_sets"`
	AvatarSetMinSlots             int    `json:"avatar_set_min_slots"`
	StoreItemSlots                int    `json:"store_item_slots"`
	StoreItemCountMin             int    `json:"store_item_count_min"`
	StoreItemCountMax             int    `json:"store_item_count_max"`
	StorePriceMin                 int    `json:"store_price_min"`
	StorePriceMax                 int    `json:"store_price_max"`
	StoreInventoryStartBox        int    `json:"store_inventory_start_box_index"`
	StoreItemAllowIDs             []int  `json:"store_item_allow_ids"`
	StoreItemDenyIDs              []int  `json:"store_item_deny_ids"`
	StoreConfirmTimeoutSec        int    `json:"store_confirm_timeout_sec"`
	FollowAccount                 string `json:"follow_account"`
	FollowRadiusX                 int    `json:"follow_radius_x"`
	FollowRadiusY                 int    `json:"follow_radius_y"`
	ShoutDelayMS                  int    `json:"shout_delay_ms"`
	ShoutSendEnabled              bool   `json:"shout_send_enabled"`
	AutoActions                   bool   `json:"auto_actions"`
	AutoTargetOnlineCount         int    `json:"auto_target_online_count"`
	AutoMoveIntervalMinSec        int    `json:"auto_move_interval_min_sec"`
	AutoMoveIntervalMaxSec        int    `json:"auto_move_interval_max_sec"`
	AutoShoutIntervalMinSec       int    `json:"auto_shout_interval_min_sec"`
	AutoShoutIntervalMaxSec       int    `json:"auto_shout_interval_max_sec"`
	AutoStoreProbabilityPercent   int    `json:"auto_store_probability_percent"`
	AutoStoreIntervalMinSec       int    `json:"auto_store_interval_min_sec"`
	AutoStoreIntervalMaxSec       int    `json:"auto_store_interval_max_sec"`
	AutoStoreDurationSec          int    `json:"auto_store_duration_sec"`
	AutoStoreTickSec              int    `json:"auto_store_tick_sec"`
	AutoStoreMaxPositionTries     int    `json:"auto_store_max_position_tries"`
	AutoStoreFailCooldownSec      int    `json:"auto_store_fail_cooldown_sec"`
	AutoGamePortStableSec         int    `json:"auto_game_port_stable_sec"`
	AutoGamePortCheckTimeoutMS    int    `json:"auto_game_port_check_timeout_ms"`
	SchedulerBadRecoverSec        int    `json:"scheduler_bad_recover_sec"`
	SchedulerBadFailures          int    `json:"scheduler_bad_failures"`
	SchedulerMetricsIntervalSec   int    `json:"scheduler_metrics_interval_sec"`
	SchedulerStoreConcurrent      int    `json:"scheduler_store_concurrent"`
	SchedulerOnlineBatchSize      int    `json:"scheduler_online_batch_size"`
	SchedulerOnlineStartRate      int    `json:"scheduler_online_start_rate"`
	SchedulerOnlineFillTimeout    int    `json:"scheduler_online_fill_timeout_sec"`
	SchedulerBreakerAbnormalPct   int    `json:"scheduler_breaker_abnormal_percent"`
	SchedulerBreakerPauseSec      int    `json:"scheduler_breaker_pause_sec"`
	SchedulerBreakerReleaseBatch  int    `json:"scheduler_breaker_release_batch"`
	SchedulerBreakerFloorPct      int    `json:"scheduler_breaker_floor_percent"`
	SchedulerPortDownReleaseBatch int    `json:"scheduler_port_down_release_batch"`
	SystemActorPollMS             int    `json:"system_actor_poll_ms"`
	SystemManualActionTimeoutSec  int    `json:"system_manual_action_timeout_sec"`
	SystemPacketRatePerSec        int    `json:"system_packet_rate_per_sec"`
}

type shoutTemplates struct {
	Channel  string   `json:"channel"`
	Type     int      `json:"type"`
	Messages []string `json:"messages"`
}

type nameTemplates struct {
	Names     []string `json:"names"`
	Prefixes  []string `json:"prefixes"`
	Middles   []string `json:"middles"`
	Suffixes  []string `json:"suffixes"`
	Pattern   string   `json:"pattern"`
	NumberMin int      `json:"number_min"`
	NumberMax int      `json:"number_max"`
}
