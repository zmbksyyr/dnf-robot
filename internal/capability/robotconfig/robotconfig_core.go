package robotconfig

import (
	"strings"
)

// ---- config.go ----
type RuntimeConfig struct {
	LevelMin                      int    `json:"level_min"`
	LevelMax                      int    `json:"level_max"`
	Jobs                          []int  `json:"jobs"`
	GrowTypes                     []int  `json:"grow_types"`
	RobotUIDStart                 int    `json:"robot_uid_start"`
	RobotUIDEnd                   int    `json:"robot_uid_end"`
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

func Clone(rc RuntimeConfig) RuntimeConfig {
	rc.Jobs = append([]int(nil), rc.Jobs...)
	rc.GrowTypes = append([]int(nil), rc.GrowTypes...)
	rc.EquipSlots = append([]int(nil), rc.EquipSlots...)
	rc.AvatarSlots = append([]int(nil), rc.AvatarSlots...)
	rc.StoreItemAllowIDs = append([]int(nil), rc.StoreItemAllowIDs...)
	rc.StoreItemDenyIDs = append([]int(nil), rc.StoreItemDenyIDs...)
	return rc
}

// ---- defaults.go ----
func Default() RuntimeConfig {
	return RuntimeConfig{
		LevelMin: 50, LevelMax: 85, Jobs: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, GrowTypes: []int{0, 1, 2},
		RobotUIDStart:     17000000,
		RobotUIDEnd:       17000999,
		NameASCIIFallback: false, NameASCIIPrefix: "twbot",
		SpawnFixed: false, SpawnVillage: 3, SpawnFallbackVillage: 1, SpawnArea: 0, SpawnXMin: 240, SpawnXMax: 1800, SpawnYMin: 180, SpawnYMax: 460,
		MoveSpeedMin: 180, MoveSpeedMax: 260, MoveType: 5, MoveSteps: 4, MoveStepDelayMS: 1200,
		LoginDelayMS: 1000, ReconnectDelayMS: 5000, MaxReconnect: 2, MaxOnlineRobots: 1000, MaxOnlinePerCommand: 1000, OnlineDispatchIntervalMS: 1000, OnlineConfirmTimeoutMS: 90000,
		DefaultMoney: 1000000, DefaultCoin: 5, InventoryCapacity: 16,
		EquipSlots: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, EquipRarityMin: 0, EquipRarityMax: 5, EquipIntensifyMin: 7, EquipIntensifyMax: 10, EquipSmithingMin: 0, EquipSmithingMax: 8,
		PreferEquipSets: true, EquipSetMinSlots: 2,
		AvatarSlots: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, MinAvatarSlots: 10, PreferAvatarSets: true, AvatarSetMinSlots: 2,
		StoreItemSlots: 1, StoreItemCountMin: 1, StoreItemCountMax: 1, StorePriceMin: 100000, StorePriceMax: 5000000, StoreInventoryStartBox: 7, StoreItemAllowIDs: []int{3037, 3031, 3032, 3034, 3035}, StoreItemDenyIDs: []int{7312, 7404, 7560, 7563, 7567, 7746},
		StoreConfirmTimeoutSec: 30,
		FollowRadiusX:          120, FollowRadiusY: 30, ShoutDelayMS: 1000, ShoutSendEnabled: true,
		AutoActions: true, AutoTargetOnlineCount: 20,
		AutoMoveIntervalMinSec: 6, AutoMoveIntervalMaxSec: 18, AutoShoutIntervalMinSec: 45, AutoShoutIntervalMaxSec: 120,
		AutoStoreProbabilityPercent: 5, AutoStoreIntervalMinSec: 120, AutoStoreIntervalMaxSec: 180, AutoStoreDurationSec: 120, AutoStoreTickSec: 10, AutoStoreMaxPositionTries: 10, AutoStoreFailCooldownSec: 60,
		AutoGamePortStableSec: 15, AutoGamePortCheckTimeoutMS: 800,
		SchedulerBadRecoverSec: 60, SchedulerBadFailures: 3, SchedulerMetricsIntervalSec: 10, SchedulerStoreConcurrent: 30, SchedulerOnlineBatchSize: 120, SchedulerOnlineStartRate: 20, SchedulerOnlineFillTimeout: 120,
		SchedulerBreakerAbnormalPct: 30, SchedulerBreakerPauseSec: 300, SchedulerBreakerReleaseBatch: 20, SchedulerBreakerFloorPct: 70, SchedulerPortDownReleaseBatch: 20,
		SystemActorPollMS: 3000, SystemManualActionTimeoutSec: 60, SystemPacketRatePerSec: 20,
	}
}

func Normalize(rc *RuntimeConfig) {
	if rc.MaxOnlineRobots <= 0 {
		rc.MaxOnlineRobots = 1000
	}
	if rc.MaxOnlinePerCommand <= 0 || rc.MaxOnlinePerCommand > rc.MaxOnlineRobots {
		rc.MaxOnlinePerCommand = rc.MaxOnlineRobots
	}
	if rc.OnlineDispatchIntervalMS < 0 {
		rc.OnlineDispatchIntervalMS = 1000
	}
	if rc.OnlineConfirmTimeoutMS < 5000 {
		rc.OnlineConfirmTimeoutMS = 5000
	}
	if rc.LoginDelayMS < 1000 {
		rc.LoginDelayMS = 1000
	}
	if rc.ReconnectDelayMS < 5000 {
		rc.ReconnectDelayMS = 5000
	}
	if rc.MaxReconnect < 0 {
		rc.MaxReconnect = 0
	}
	if rc.MaxReconnect > 10 {
		rc.MaxReconnect = 10
	}
	if rc.ShoutDelayMS < 0 {
		rc.ShoutDelayMS = 0
	}
	if rc.MoveSteps <= 0 {
		rc.MoveSteps = 4
	}
	if rc.MoveSteps > 12 {
		rc.MoveSteps = 12
	}
	if rc.MoveStepDelayMS < 0 {
		rc.MoveStepDelayMS = 0
	}
	if rc.InventoryCapacity <= 0 {
		rc.InventoryCapacity = 16
	}
	if len(rc.Jobs) == 0 {
		rc.Jobs = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	}
	if len(rc.GrowTypes) == 0 {
		rc.GrowTypes = []int{0, 1, 2}
	}
	if rc.RobotUIDStart < 100000 {
		rc.RobotUIDStart = 17000000
	}
	if rc.RobotUIDEnd < rc.RobotUIDStart {
		rc.RobotUIDEnd = rc.RobotUIDStart + 999
	}
	if rc.EquipRarityMax < rc.EquipRarityMin {
		rc.EquipRarityMin, rc.EquipRarityMax = rc.EquipRarityMax, rc.EquipRarityMin
	}
	if rc.EquipIntensifyMax < rc.EquipIntensifyMin {
		rc.EquipIntensifyMin, rc.EquipIntensifyMax = rc.EquipIntensifyMax, rc.EquipIntensifyMin
	}
	if rc.EquipSmithingMax < rc.EquipSmithingMin {
		rc.EquipSmithingMin, rc.EquipSmithingMax = rc.EquipSmithingMax, rc.EquipSmithingMin
	}
	if rc.EquipSetMinSlots <= 1 {
		rc.EquipSetMinSlots = 2
	}
	if len(rc.EquipSlots) > 0 && rc.EquipSetMinSlots > len(rc.EquipSlots) {
		rc.EquipSetMinSlots = len(rc.EquipSlots)
	}
	if rc.MinAvatarSlots < 0 {
		rc.MinAvatarSlots = 0
	}
	if rc.AvatarSetMinSlots <= 1 {
		rc.AvatarSetMinSlots = 2
	}
	if len(rc.AvatarSlots) > 0 && rc.AvatarSetMinSlots > len(rc.AvatarSlots) {
		rc.AvatarSetMinSlots = len(rc.AvatarSlots)
	}
	if rc.AutoMoveIntervalMinSec <= 0 {
		rc.AutoMoveIntervalMinSec = 6
	}
	if rc.AutoMoveIntervalMaxSec < rc.AutoMoveIntervalMinSec {
		rc.AutoMoveIntervalMaxSec = rc.AutoMoveIntervalMinSec + 8
	}
	if rc.AutoShoutIntervalMinSec <= 0 {
		rc.AutoShoutIntervalMinSec = 45
	}
	if rc.AutoShoutIntervalMaxSec < rc.AutoShoutIntervalMinSec {
		rc.AutoShoutIntervalMaxSec = rc.AutoShoutIntervalMinSec + 60
	}
	if rc.AutoTargetOnlineCount < 0 {
		rc.AutoTargetOnlineCount = 0
	}
	if rc.AutoTargetOnlineCount > rc.MaxOnlineRobots {
		rc.AutoTargetOnlineCount = rc.MaxOnlineRobots
	}
	if rc.AutoGamePortStableSec <= 0 {
		rc.AutoGamePortStableSec = 15
	}
	if rc.AutoGamePortStableSec > 300 {
		rc.AutoGamePortStableSec = 300
	}
	if rc.AutoGamePortCheckTimeoutMS <= 0 {
		rc.AutoGamePortCheckTimeoutMS = 800
	}
	if rc.AutoGamePortCheckTimeoutMS > 10000 {
		rc.AutoGamePortCheckTimeoutMS = 10000
	}
	if rc.AutoStoreProbabilityPercent < 0 {
		rc.AutoStoreProbabilityPercent = 0
	}
	if rc.AutoStoreProbabilityPercent > 100 {
		rc.AutoStoreProbabilityPercent = 100
	}
	if rc.AutoStoreIntervalMinSec <= 0 {
		rc.AutoStoreIntervalMinSec = 60
	}
	if rc.AutoStoreIntervalMaxSec < rc.AutoStoreIntervalMinSec {
		rc.AutoStoreIntervalMaxSec = rc.AutoStoreIntervalMinSec + 120
	}
	if rc.AutoStoreDurationSec <= 0 {
		rc.AutoStoreDurationSec = 120
	}
	if rc.AutoStoreDurationSec < 60 {
		rc.AutoStoreDurationSec = 60
	}
	if rc.AutoStoreDurationSec > 86400 {
		rc.AutoStoreDurationSec = 86400
	}
	if rc.AutoStoreTickSec <= 0 {
		rc.AutoStoreTickSec = 10
	}
	if rc.AutoStoreTickSec > 300 {
		rc.AutoStoreTickSec = 300
	}
	if rc.AutoStoreMaxPositionTries <= 0 {
		rc.AutoStoreMaxPositionTries = 10
	}
	if rc.AutoStoreMaxPositionTries > 10000 {
		rc.AutoStoreMaxPositionTries = 10000
	}
	if rc.AutoStoreFailCooldownSec <= 0 {
		rc.AutoStoreFailCooldownSec = 60
	}
	if rc.AutoStoreFailCooldownSec > 3600 {
		rc.AutoStoreFailCooldownSec = 3600
	}
	if rc.SchedulerBadRecoverSec <= 0 {
		rc.SchedulerBadRecoverSec = 60
	}
	if rc.SchedulerBadFailures <= 0 {
		rc.SchedulerBadFailures = 3
	}
	if rc.SchedulerMetricsIntervalSec <= 0 {
		rc.SchedulerMetricsIntervalSec = 10
	}
	if rc.SchedulerMetricsIntervalSec > 300 {
		rc.SchedulerMetricsIntervalSec = 300
	}
	if rc.SchedulerStoreConcurrent <= 0 {
		rc.SchedulerStoreConcurrent = 30
	}
	if rc.SchedulerOnlineBatchSize > 120 {
		rc.SchedulerOnlineBatchSize = 120
	}
	if rc.SchedulerOnlineStartRate <= 0 {
		rc.SchedulerOnlineStartRate = 20
	}
	if rc.SchedulerOnlineStartRate > 60 {
		rc.SchedulerOnlineStartRate = 60
	}
	if rc.SchedulerOnlineFillTimeout <= 0 {
		rc.SchedulerOnlineFillTimeout = 60
	}
	if rc.SchedulerBreakerAbnormalPct <= 0 {
		rc.SchedulerBreakerAbnormalPct = 30
	}
	if rc.SchedulerBreakerAbnormalPct > 100 {
		rc.SchedulerBreakerAbnormalPct = 100
	}
	if rc.SchedulerBreakerPauseSec <= 0 {
		rc.SchedulerBreakerPauseSec = 300
	}
	if rc.SchedulerBreakerPauseSec < 30 {
		rc.SchedulerBreakerPauseSec = 30
	}
	if rc.SchedulerBreakerPauseSec > 3600 {
		rc.SchedulerBreakerPauseSec = 3600
	}
	if rc.SchedulerBreakerReleaseBatch <= 0 {
		rc.SchedulerBreakerReleaseBatch = 20
	}
	if rc.SchedulerBreakerReleaseBatch > 120 {
		rc.SchedulerBreakerReleaseBatch = 120
	}
	if rc.SchedulerBreakerFloorPct < 0 {
		rc.SchedulerBreakerFloorPct = 0
	}
	if rc.SchedulerBreakerFloorPct > 100 {
		rc.SchedulerBreakerFloorPct = 100
	}
	if rc.SchedulerPortDownReleaseBatch <= 0 {
		rc.SchedulerPortDownReleaseBatch = 20
	}
	if rc.SchedulerPortDownReleaseBatch > 120 {
		rc.SchedulerPortDownReleaseBatch = 120
	}
	if rc.SystemActorPollMS <= 0 {
		rc.SystemActorPollMS = 1000
	}
	if rc.SystemActorPollMS < 100 {
		rc.SystemActorPollMS = 100
	}
	if rc.SystemActorPollMS > 10000 {
		rc.SystemActorPollMS = 10000
	}
	if rc.SystemManualActionTimeoutSec <= 0 {
		rc.SystemManualActionTimeoutSec = 60
	}
	if rc.SystemManualActionTimeoutSec > 3600 {
		rc.SystemManualActionTimeoutSec = 3600
	}
	if rc.SystemPacketRatePerSec <= 0 {
		rc.SystemPacketRatePerSec = 20
	}
	if rc.StoreItemSlots <= 0 {
		rc.StoreItemSlots = 1
	}
	if rc.StoreItemSlots > 24 {
		rc.StoreItemSlots = 24
	}
	if rc.StoreItemCountMin <= 0 {
		rc.StoreItemCountMin = 1
	}
	if rc.StoreItemCountMax <= 0 {
		rc.StoreItemCountMax = rc.StoreItemCountMin
	}
	if rc.StoreItemCountMax < rc.StoreItemCountMin {
		rc.StoreItemCountMin, rc.StoreItemCountMax = rc.StoreItemCountMax, rc.StoreItemCountMin
	}
	if rc.StorePriceMin <= 0 {
		rc.StorePriceMin = 100000
	}
	if rc.StorePriceMax < rc.StorePriceMin {
		rc.StorePriceMin, rc.StorePriceMax = rc.StorePriceMax, rc.StorePriceMin
	}
	if rc.StoreInventoryStartBox <= 0 || rc.StoreInventoryStartBox == 105 {
		rc.StoreInventoryStartBox = 7
	}
	if rc.StoreInventoryStartBox > 240 {
		rc.StoreInventoryStartBox = 240
	}
	if rc.StoreConfirmTimeoutSec <= 0 {
		rc.StoreConfirmTimeoutSec = 30
	}
	if rc.StoreConfirmTimeoutSec > 35 {
		rc.StoreConfirmTimeoutSec = 35
	}
	if rc.SpawnVillage < 1 {
		rc.SpawnVillage = 1
	}
	if rc.SpawnVillage > 3 {
		rc.SpawnVillage = 3
	}
}

// ---- scheduler_policy.go ----
func TargetCapacity(rc RuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	return target
}

func CreateRoom(rc RuntimeConfig, existing int) int {
	room := TargetCapacity(rc) - existing
	if room < 0 {
		return 0
	}
	return room
}

func ScaleUpBatch(rc RuntimeConfig) int {
	batch := rc.SchedulerOnlineBatchSize
	if batch < 0 {
		return 0
	}
	if batch <= 0 {
		batch = 20
	}
	if batch > 120 {
		batch = 120
	}
	return batch
}

func PendingActorLimit(target int, rc RuntimeConfig) int {
	if target <= 0 {
		return 1
	}
	limit := OnlineStartRate(rc) * 8
	if limit < target/10 {
		limit = target / 10
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 120 {
		limit = 120
	}
	return limit
}

func ScaleDownBatch(current, target int) int {
	delta := current - target
	if delta <= 0 {
		return 0
	}
	batch := current / 25
	if current%25 != 0 {
		batch++
	}
	if batch < 5 {
		batch = 5
	}
	if batch > 50 {
		batch = 50
	}
	if batch > delta {
		batch = delta
	}
	return batch
}

func OnlineStartRate(rc RuntimeConfig) int {
	rate := rc.SchedulerOnlineStartRate
	if rate < 0 {
		return 0
	}
	if rate <= 0 {
		rate = 20
	}
	if rate > 60 {
		rate = 60
	}
	return rate
}

func OnlineStartRateForNeed(need int, rc RuntimeConfig) int {
	rate := OnlineStartRate(rc)
	if rate <= 0 {
		return 0
	}
	if need <= 0 {
		return rate
	}
	timeout := rc.SchedulerOnlineFillTimeout
	if timeout <= 0 {
		timeout = 60
	}
	required := (need + timeout - 1) / timeout
	if required > rate {
		rate = required
	}
	if rate > 60 {
		return 60
	}
	return rate
}

func BreakerActorFloor(rc RuntimeConfig) int {
	target := TargetCapacity(rc)
	floorPct := rc.SchedulerBreakerFloorPct
	if floorPct < 0 {
		floorPct = 0
	}
	if floorPct > 100 {
		floorPct = 100
	}
	floor := target * floorPct / 100
	if target <= 50 && floor < target {
		return target
	}
	return floor
}

func NormalizedTarget(rc RuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	if target <= 0 {
		target = 20
	}
	return target
}

func TargetScale(target int) int {
	scale := target / 100
	if target%100 != 0 {
		scale++
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 10 {
		scale = 10
	}
	return scale
}

func Clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ---- text.go ----
func UpdateINIText(text string, values map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	section := ""
	seen := make(map[string]bool, len(values))
	sectionLine := make(map[string]int)
	lastInSection := make(map[string]int)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if end := strings.IndexByte(trimmed, ']'); end > 1 {
				section = strings.TrimSpace(trimmed[1:end])
				sectionLine[section] = i
				lastInSection[section] = i
			}
			continue
		}
		if section != "" && trimmed != "" {
			lastInSection[section] = i
		}
		if section == "" || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if idx := strings.IndexByte(trimmed, '='); idx > 0 {
			key := strings.TrimSpace(trimmed[:idx])
			fullKey := section + "." + key
			value, ok := values[fullKey]
			if !ok {
				continue
			}
			prefix := line[:strings.Index(line, "=")+1]
			lines[i] = prefix + " " + value
			seen[fullKey] = true
		}
	}
	for fullKey, value := range values {
		if seen[fullKey] {
			continue
		}
		parts := strings.SplitN(fullKey, ".", 2)
		if len(parts) != 2 {
			continue
		}
		section, key := parts[0], parts[1]
		line := key + " = " + value
		if _, ok := sectionLine[section]; !ok {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, "["+section+"]", line)
			sectionLine[section] = len(lines) - 2
			lastInSection[section] = len(lines) - 1
			continue
		}
		insertAt := lastInSection[section] + 1
		lines = append(lines[:insertAt], append([]string{line}, lines[insertAt:]...)...)
		for s, idx := range sectionLine {
			if idx >= insertAt {
				sectionLine[s] = idx + 1
			}
		}
		for s, idx := range lastInSection {
			if idx >= insertAt {
				lastInSection[s] = idx + 1
			}
		}
		lastInSection[section] = insertAt
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func PublicText(text string) string {
	hidden := map[string]bool{
		"auto.auto_game_port_stable_sec":       true,
		"auto.auto_game_port_check_timeout_ms": true,
		"auto.auto_move_interval_min_sec":      true,
		"auto.auto_move_interval_max_sec":      true,
		"auto.auto_shout_interval_min_sec":     true,
		"auto.auto_shout_interval_max_sec":     true,
		"auto.auto_store_probability_percent":  true,
		"auto.auto_store_interval_min_sec":     true,
		"auto.auto_store_interval_max_sec":     true,
		"auto.auto_store_duration_sec":         true,
		"auto.auto_store_tick_sec":             true,
		"auto.auto_store_max_position_tries":   true,
		"auto.auto_store_fail_cooldown_sec":    true,
		"scheduler.bad_recover_sec":            true,
		"scheduler.bad_failures":               true,
		"scheduler.metrics_interval_sec":       true,
		"scheduler.store_concurrent":           true,
		"scheduler.online_batch_size":          true,
		"scheduler.online_start_rate":          true,
		"scheduler.online_fill_timeout_sec":    true,
		"scheduler.breaker_abnormal_percent":   true,
		"scheduler.breaker_pause_sec":          true,
		"scheduler.breaker_release_batch":      true,
		"scheduler.breaker_floor_percent":      true,
		"scheduler.port_down_release_batch":    true,
		"system.actor_poll_ms":                 true,
		"system.manual_action_timeout_sec":     true,
		"system.packet_rate_per_sec":           true,
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if end := strings.IndexByte(trimmed, ']'); end > 1 {
				section = strings.TrimSpace(trimmed[1:end])
			}
			out = append(out, line)
			continue
		}
		if section != "" && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			if idx := strings.IndexByte(trimmed, '='); idx > 0 {
				key := strings.TrimSpace(trimmed[:idx])
				if hidden[section+"."+key] {
					continue
				}
			}
		}
		out = append(out, line)
	}
	result := strings.Join(out, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}
