package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"robot/internal/config"
)

func (m *RobotManager) loadRobotConfig() robotRuntimeConfig {
	configPath := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	configMod := fileModTime(configPath)
	m.cacheMu.Lock()
	if m.configCached && m.configMod.Equal(configMod) {
		rc := cloneRobotRuntimeConfig(m.configCache)
		m.cacheMu.Unlock()
		return rc
	}
	m.cacheMu.Unlock()

	rc := robotRuntimeConfig{
		LevelMin: 50, LevelMax: 85, Jobs: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, GrowTypes: []int{0, 1, 2},
		RobotUIDStart:     17000000,
		NameASCIIFallback: false, NameASCIIPrefix: "twbot",
		SpawnFixed: false, SpawnVillage: 3, SpawnFallbackVillage: 1, SpawnArea: 0, SpawnXMin: 240, SpawnXMax: 1800, SpawnYMin: 180, SpawnYMax: 460,
		MoveSpeedMin: 180, MoveSpeedMax: 260, MoveType: 5, MoveSteps: 4, MoveStepDelayMS: 1200,
		LoginDelayMS: 1000, ReconnectDelayMS: 5000, MaxReconnect: 3, MaxOnlineRobots: 1000, MaxOnlinePerCommand: 1000, OnlineDispatchIntervalMS: 1000, OnlineConfirmTimeoutMS: 60000,
		DefaultMoney: 2000000000, DefaultCoin: 5, InventoryCapacity: 16,
		EquipSlots: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, EquipRarityMin: 0, EquipRarityMax: 5, EquipIntensifyMin: 7, EquipIntensifyMax: 10, EquipSmithingMin: 0, EquipSmithingMax: 8,
		PreferEquipSets: true, EquipSetMinSlots: 2,
		AvatarSlots: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, MinAvatarSlots: 10, PreferAvatarSets: true, AvatarSetMinSlots: 2,
		StoreItemSlots: 3, StoreItemCountMin: 2, StoreItemCountMax: 20, StorePriceMin: 100000, StorePriceMax: 5000000, StoreInventoryStartBox: 105, StoreItemAllowIDs: []int{3037, 3031, 3032, 3034, 3035}, StoreItemDenyIDs: []int{7312, 7404, 7560, 7563, 7567, 7746},
		StoreConfirmTimeoutSec: 30,
		FollowRadiusX:          120, FollowRadiusY: 30, ShoutDelayMS: 1000, ShoutSendEnabled: true,
		AutoActions: true, AutoTargetOnlineCount: 20,
		AutoMoveIntervalMinSec: 6, AutoMoveIntervalMaxSec: 18, AutoShoutIntervalMinSec: 45, AutoShoutIntervalMaxSec: 120,
		AutoStoreProbabilityPercent: 5, AutoStoreIntervalMinSec: 60, AutoStoreIntervalMaxSec: 180, AutoStoreDurationSec: 120, AutoStoreTickSec: 10, AutoStoreMaxPositionTries: 10, AutoStoreFailCooldownSec: 60,
		AutoGamePortStableSec: 15, AutoGamePortCheckTimeoutMS: 800,
		SchedulerBadRecoverSec: 60, SchedulerBadFailures: 3, SchedulerMetricsIntervalSec: 10, SchedulerStoreConcurrent: 30, SchedulerOnlineBatchSize: 120, SchedulerOnlineStartRate: 20, SchedulerOnlineFillTimeout: 60,
		SchedulerBreakerAbnormalPct: 30, SchedulerBreakerPauseSec: 300, SchedulerBreakerReleaseBatch: 20, SchedulerBreakerFloorPct: 70, SchedulerPortDownReleaseBatch: 20,
		SystemActorPollMS: 1000, SystemManualActionTimeoutSec: 60, SystemPacketRatePerSec: 20,
	}
	m.loadRobotConfigINI(&rc)
	m.applyAdaptiveSchedulerConfig(&rc)
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
		rc.StoreItemSlots = 3
	}
	if rc.StoreItemSlots > 24 {
		rc.StoreItemSlots = 24
	}
	if rc.StoreItemCountMin <= 0 {
		rc.StoreItemCountMin = 2
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
	if rc.StoreInventoryStartBox <= 0 {
		rc.StoreInventoryStartBox = 105
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
	m.cacheMu.Lock()
	m.configCache = cloneRobotRuntimeConfig(rc)
	m.configMod = configMod
	m.configCached = true
	m.cacheMu.Unlock()
	return cloneRobotRuntimeConfig(rc)
}

func (m *RobotManager) loadRobotConfigINI(rc *robotRuntimeConfig) {
	ini, err := config.Load(filepath.Join(m.cfg.ConfigDir, "robot_config.ini"))
	if err != nil {
		return
	}
	rc.LevelMin = ini.GetInt("create", "level_min", rc.LevelMin)
	rc.LevelMax = ini.GetInt("create", "level_max", rc.LevelMax)
	rc.Jobs = iniIntList(ini, "create", "jobs", rc.Jobs)
	rc.GrowTypes = iniIntList(ini, "create", "grow_types", rc.GrowTypes)
	rc.RobotUIDStart = ini.GetInt("create", "robot_uid_start", rc.RobotUIDStart)
	rc.NameASCIIFallback = iniBool(ini, "create", "name_ascii_fallback", rc.NameASCIIFallback)
	rc.NameASCIIPrefix = ini.GetString("create", "name_ascii_prefix", rc.NameASCIIPrefix)
	rc.DefaultMoney = ini.GetInt("create", "default_money", rc.DefaultMoney)
	rc.DefaultCoin = ini.GetInt("create", "default_coin", rc.DefaultCoin)
	rc.InventoryCapacity = ini.GetInt("create", "inventory_capacity", rc.InventoryCapacity)

	rc.SpawnFixed = iniBool(ini, "spawn", "spawn_fixed", rc.SpawnFixed)
	rc.SpawnVillage = ini.GetInt("spawn", "spawn_village", rc.SpawnVillage)
	rc.SpawnFallbackVillage = ini.GetInt("spawn", "spawn_fallback_village", rc.SpawnFallbackVillage)
	rc.SpawnArea = ini.GetInt("spawn", "spawn_area", rc.SpawnArea)
	rc.SpawnXMin = ini.GetInt("spawn", "spawn_x_min", rc.SpawnXMin)
	rc.SpawnXMax = ini.GetInt("spawn", "spawn_x_max", rc.SpawnXMax)
	rc.SpawnYMin = ini.GetInt("spawn", "spawn_y_min", rc.SpawnYMin)
	rc.SpawnYMax = ini.GetInt("spawn", "spawn_y_max", rc.SpawnYMax)

	rc.MoveSpeedMin = ini.GetInt("move", "move_speed_min", rc.MoveSpeedMin)
	rc.MoveSpeedMax = ini.GetInt("move", "move_speed_max", rc.MoveSpeedMax)
	rc.MoveType = ini.GetInt("move", "move_type", rc.MoveType)
	rc.MoveSteps = ini.GetInt("move", "move_steps", rc.MoveSteps)
	rc.MoveStepDelayMS = ini.GetInt("move", "move_step_delay_ms", rc.MoveStepDelayMS)

	rc.LoginDelayMS = ini.GetInt("online", "login_delay_ms", rc.LoginDelayMS)
	rc.ReconnectDelayMS = ini.GetInt("online", "reconnect_delay_ms", rc.ReconnectDelayMS)
	rc.MaxReconnect = ini.GetInt("online", "max_reconnect", rc.MaxReconnect)
	rc.MaxOnlineRobots = ini.GetInt("online", "max_online_robots", rc.MaxOnlineRobots)
	rc.MaxOnlinePerCommand = ini.GetInt("online", "max_online_per_command", rc.MaxOnlinePerCommand)
	rc.OnlineDispatchIntervalMS = ini.GetInt("online", "online_dispatch_interval_ms", rc.OnlineDispatchIntervalMS)
	rc.OnlineConfirmTimeoutMS = ini.GetInt("online", "online_confirm_timeout_ms", rc.OnlineConfirmTimeoutMS)

	rc.EquipSlots = iniIntList(ini, "equipment", "equip_slots", rc.EquipSlots)
	rc.EquipRarityMin = ini.GetInt("equipment", "equip_rarity_min", rc.EquipRarityMin)
	rc.EquipRarityMax = ini.GetInt("equipment", "equip_rarity_max", rc.EquipRarityMax)
	rc.EquipIntensifyMin = ini.GetInt("equipment", "equip_intensify_min", rc.EquipIntensifyMin)
	rc.EquipIntensifyMax = ini.GetInt("equipment", "equip_intensify_max", rc.EquipIntensifyMax)
	rc.EquipSmithingMin = ini.GetInt("equipment", "equip_smithing_min", rc.EquipSmithingMin)
	rc.EquipSmithingMax = ini.GetInt("equipment", "equip_smithing_max", rc.EquipSmithingMax)
	rc.PreferEquipSets = iniBool(ini, "equipment", "prefer_equip_sets", rc.PreferEquipSets)
	rc.EquipSetMinSlots = ini.GetInt("equipment", "equip_set_min_slots", rc.EquipSetMinSlots)

	rc.AvatarSlots = iniIntList(ini, "avatar", "avatar_slots", rc.AvatarSlots)
	rc.MinAvatarSlots = ini.GetInt("avatar", "min_avatar_slots", rc.MinAvatarSlots)
	rc.PreferAvatarSets = iniBool(ini, "avatar", "prefer_avatar_sets", rc.PreferAvatarSets)
	rc.AvatarSetMinSlots = ini.GetInt("avatar", "avatar_set_min_slots", rc.AvatarSetMinSlots)

	rc.StoreItemAllowIDs = iniIntList(ini, "store", "store_item_allow_ids", rc.StoreItemAllowIDs)
	rc.StoreItemDenyIDs = iniIntList(ini, "store", "store_item_deny_ids", rc.StoreItemDenyIDs)
	rc.StoreItemSlots = ini.GetInt("store", "store_item_slots", rc.StoreItemSlots)
	rc.StoreItemCountMin = ini.GetInt("store", "store_item_count_min", rc.StoreItemCountMin)
	rc.StoreItemCountMax = ini.GetInt("store", "store_item_count_max", rc.StoreItemCountMax)
	rc.StorePriceMin = ini.GetInt("store", "store_price_min", rc.StorePriceMin)
	rc.StorePriceMax = ini.GetInt("store", "store_price_max", rc.StorePriceMax)
	rc.StoreInventoryStartBox = ini.GetInt("store", "store_inventory_start_box_index", rc.StoreInventoryStartBox)
	rc.StoreConfirmTimeoutSec = ini.GetInt("store", "store_confirm_timeout_sec", rc.StoreConfirmTimeoutSec)

	rc.FollowAccount = ini.GetString("follow", "follow_account", rc.FollowAccount)
	rc.FollowRadiusX = ini.GetInt("follow", "follow_radius_x", rc.FollowRadiusX)
	rc.FollowRadiusY = ini.GetInt("follow", "follow_radius_y", rc.FollowRadiusY)

	rc.ShoutDelayMS = ini.GetInt("shout", "shout_delay_ms", rc.ShoutDelayMS)
	rc.ShoutSendEnabled = iniBool(ini, "shout", "shout_send_enabled", rc.ShoutSendEnabled)

	rc.AutoActions = iniBool(ini, "auto", "auto_actions", rc.AutoActions)
	rc.AutoTargetOnlineCount = ini.GetInt("auto", "auto_target_online_count", rc.AutoTargetOnlineCount)
	rc.AutoMoveIntervalMinSec = ini.GetInt("auto", "auto_move_interval_min_sec", rc.AutoMoveIntervalMinSec)
	rc.AutoMoveIntervalMaxSec = ini.GetInt("auto", "auto_move_interval_max_sec", rc.AutoMoveIntervalMaxSec)
	rc.AutoShoutIntervalMinSec = ini.GetInt("auto", "auto_shout_interval_min_sec", rc.AutoShoutIntervalMinSec)
	rc.AutoShoutIntervalMaxSec = ini.GetInt("auto", "auto_shout_interval_max_sec", rc.AutoShoutIntervalMaxSec)
	rc.AutoStoreProbabilityPercent = ini.GetInt("auto", "auto_store_probability_percent", rc.AutoStoreProbabilityPercent)
	rc.AutoStoreIntervalMinSec = ini.GetInt("auto", "auto_store_interval_min_sec", rc.AutoStoreIntervalMinSec)
	rc.AutoStoreIntervalMaxSec = ini.GetInt("auto", "auto_store_interval_max_sec", rc.AutoStoreIntervalMaxSec)
	rc.AutoStoreDurationSec = ini.GetInt("auto", "auto_store_duration_sec", rc.AutoStoreDurationSec)
	rc.AutoStoreTickSec = ini.GetInt("auto", "auto_store_tick_sec", rc.AutoStoreTickSec)
	rc.AutoStoreMaxPositionTries = ini.GetInt("auto", "auto_store_max_position_tries", rc.AutoStoreMaxPositionTries)
	rc.AutoStoreFailCooldownSec = ini.GetInt("auto", "auto_store_fail_cooldown_sec", rc.AutoStoreFailCooldownSec)
	rc.AutoGamePortStableSec = ini.GetInt("auto", "auto_game_port_stable_sec", rc.AutoGamePortStableSec)
	rc.AutoGamePortCheckTimeoutMS = ini.GetInt("auto", "auto_game_port_check_timeout_ms", rc.AutoGamePortCheckTimeoutMS)

	rc.SchedulerBadRecoverSec = ini.GetInt("scheduler", "bad_recover_sec", rc.SchedulerBadRecoverSec)
	rc.SchedulerBadFailures = ini.GetInt("scheduler", "bad_failures", rc.SchedulerBadFailures)
	rc.SchedulerMetricsIntervalSec = ini.GetInt("scheduler", "metrics_interval_sec", rc.SchedulerMetricsIntervalSec)
	rc.SchedulerStoreConcurrent = ini.GetInt("scheduler", "store_concurrent", rc.SchedulerStoreConcurrent)
	rc.SchedulerOnlineBatchSize = ini.GetInt("scheduler", "online_batch_size", rc.SchedulerOnlineBatchSize)
	rc.SchedulerOnlineStartRate = ini.GetInt("scheduler", "online_start_rate", rc.SchedulerOnlineStartRate)
	rc.SchedulerOnlineFillTimeout = ini.GetInt("scheduler", "online_fill_timeout_sec", rc.SchedulerOnlineFillTimeout)
	rc.SchedulerBreakerAbnormalPct = ini.GetInt("scheduler", "breaker_abnormal_percent", rc.SchedulerBreakerAbnormalPct)
	rc.SchedulerBreakerPauseSec = ini.GetInt("scheduler", "breaker_pause_sec", rc.SchedulerBreakerPauseSec)
	rc.SchedulerBreakerReleaseBatch = ini.GetInt("scheduler", "breaker_release_batch", rc.SchedulerBreakerReleaseBatch)
	rc.SchedulerBreakerFloorPct = ini.GetInt("scheduler", "breaker_floor_percent", rc.SchedulerBreakerFloorPct)
	rc.SchedulerPortDownReleaseBatch = ini.GetInt("scheduler", "port_down_release_batch", rc.SchedulerPortDownReleaseBatch)

	rc.SystemActorPollMS = ini.GetInt("system", "actor_poll_ms", rc.SystemActorPollMS)
	rc.SystemManualActionTimeoutSec = ini.GetInt("system", "manual_action_timeout_sec", rc.SystemManualActionTimeoutSec)
	rc.SystemPacketRatePerSec = ini.GetInt("system", "packet_rate_per_sec", rc.SystemPacketRatePerSec)
}

func iniBool(ini *config.INIConfig, section, key string, fallback bool) bool {
	raw := strings.TrimSpace(ini.GetString(section, key, ""))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err == nil {
		return v
	}
	switch strings.ToLower(raw) {
	case "yes", "on", "enabled":
		return true
	case "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}

func iniIntList(ini *config.INIConfig, section, key string, fallback []int) []int {
	raw := strings.TrimSpace(ini.GetString(section, key, ""))
	if raw == "" {
		return fallback
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	out := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func (m *RobotManager) loadShoutTemplates() shoutTemplates {
	mod := configFileModFallback(m.cfg.ConfigDir, "robot_shout_templates.json", "shout_templates.json")
	m.cacheMu.Lock()
	if m.shoutCached && m.shoutMod.Equal(mod) {
		t := cloneShoutTemplates(m.shoutCache)
		m.cacheMu.Unlock()
		return t
	}
	m.cacheMu.Unlock()

	t := shoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}
	data, err := readConfigFileFallback(m.cfg.ConfigDir, "robot_shout_templates.json", "shout_templates.json")
	if err == nil {
		var messages []string
		if json.Unmarshal(data, &messages) == nil {
			t.Messages = dedupeStrings(messages)
		} else {
			_ = json.Unmarshal(data, &t)
			t.Messages = dedupeStrings(t.Messages)
		}
	}
	if t.Type == 0 {
		t.Type = 3
	}
	if len(t.Messages) == 0 {
		t.Messages = []string{"hello"}
	}
	m.cacheMu.Lock()
	m.shoutCache = cloneShoutTemplates(t)
	m.shoutMod = mod
	m.shoutCached = true
	m.cacheMu.Unlock()
	return cloneShoutTemplates(t)
}

func (m *RobotManager) loadNameTemplates() nameTemplates {
	t := nameTemplates{
		Prefixes:  []string{"Bot", "Star", "Moon", "Sky"},
		Middles:   []string{"Blade", "Wind", "Light", "Fire"},
		Suffixes:  []string{"One", "Two", "X", "Z"},
		Pattern:   "{prefix}{middle}{suffix}{number}",
		NumberMin: 10,
		NumberMax: 99,
	}
	data, err := readConfigFileFallback(m.cfg.ConfigDir, "robot_name_templates.json", "name_templates.json")
	if err == nil {
		if names := parseStringListJSON(data); len(names) > 0 {
			t.Names = names
		} else {
			_ = json.Unmarshal(data, &t)
		}
		t.Names = dedupeStrings(t.Names)
	}
	if len(t.Names) > 0 {
		return t
	}
	if len(t.Prefixes) == 0 {
		t.Prefixes = []string{"Bot"}
	}
	if len(t.Middles) == 0 {
		t.Middles = []string{"Name"}
	}
	if len(t.Suffixes) == 0 {
		t.Suffixes = []string{"X"}
	}
	if t.Pattern == "" {
		t.Pattern = "{prefix}{middle}{suffix}{number}"
	}
	return t
}

func (m *RobotManager) loadMapCatalog() []mapCatalogItem {
	mod := configFileModFallback(m.cfg.ConfigDir, "pvf_map_catalog.json", "map_catalog.json")
	m.cacheMu.Lock()
	if m.mapCached && m.mapMod.Equal(mod) {
		maps := m.mapCache
		m.cacheMu.Unlock()
		return maps
	}
	m.cacheMu.Unlock()

	data, err := readConfigFileFallback(m.cfg.ConfigDir, "pvf_map_catalog.json", "map_catalog.json")
	if err != nil {
		return nil
	}
	var maps []mapCatalogItem
	if json.Unmarshal(data, &maps) != nil {
		return nil
	}
	m.cacheMu.Lock()
	m.mapCache = maps
	m.mapMod = mod
	m.mapCached = true
	m.cacheMu.Unlock()
	return maps
}

func readConfigFileFallback(configDir string, names ...string) ([]byte, error) {
	var lastErr error
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func fileModTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

func configFileModFallback(configDir string, names ...string) time.Time {
	for _, name := range names {
		if mod := fileModTime(filepath.Join(configDir, name)); !mod.IsZero() {
			return mod
		}
	}
	return time.Time{}
}

func cloneRobotRuntimeConfig(rc robotRuntimeConfig) robotRuntimeConfig {
	rc.Jobs = append([]int(nil), rc.Jobs...)
	rc.GrowTypes = append([]int(nil), rc.GrowTypes...)
	rc.EquipSlots = append([]int(nil), rc.EquipSlots...)
	rc.AvatarSlots = append([]int(nil), rc.AvatarSlots...)
	rc.StoreItemAllowIDs = append([]int(nil), rc.StoreItemAllowIDs...)
	rc.StoreItemDenyIDs = append([]int(nil), rc.StoreItemDenyIDs...)
	return rc
}

func cloneShoutTemplates(t shoutTemplates) shoutTemplates {
	t.Messages = append([]string(nil), t.Messages...)
	return t
}

func safeRobotShoutMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "hello"
	}
	const maxBytes = 72
	var b strings.Builder
	for _, r := range msg {
		if r < 0x20 {
			continue
		}
		next := string(r)
		if b.Len()+len(next) > maxBytes {
			break
		}
		b.WriteString(next)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "hello"
	}
	return out
}

func (m *RobotManager) invalidateRobotConfigCache() {
	m.cacheMu.Lock()
	m.configCached = false
	m.cacheMu.Unlock()
}

func parseStringListJSON(data []byte) []string {
	var list []string
	if json.Unmarshal(data, &list) == nil {
		return dedupeStrings(list)
	}
	var obj struct {
		Names    []string `json:"names"`
		Messages []string `json:"messages"`
	}
	if json.Unmarshal(data, &obj) == nil {
		if len(obj.Names) > 0 {
			return dedupeStrings(obj.Names)
		}
		return dedupeStrings(obj.Messages)
	}
	return nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
