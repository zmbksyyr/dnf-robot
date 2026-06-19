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

	rc := defaultRobotRuntimeConfig()
	m.loadRobotConfigINI(&rc)
	m.applyAdaptiveSchedulerConfig(&rc)
	normalizeRobotRuntimeConfig(&rc)
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
	path := filepath.Join(m.cfg.ConfigDir, "robot_shout_templates.json")
	mod := fileModTime(path)
	m.cacheMu.Lock()
	if m.shoutCached && m.shoutMod.Equal(mod) {
		t := cloneShoutTemplates(m.shoutCache)
		m.cacheMu.Unlock()
		return t
	}
	m.cacheMu.Unlock()

	t := shoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}
	data, err := os.ReadFile(path)
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
	data, err := os.ReadFile(filepath.Join(m.cfg.ConfigDir, "robot_name_templates.json"))
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
	path := filepath.Join(m.cfg.ConfigDir, "pvf_map_catalog.json")
	mod := fileModTime(path)
	m.cacheMu.Lock()
	if m.mapCached && m.mapMod.Equal(mod) {
		maps := m.mapCache
		m.cacheMu.Unlock()
		return maps
	}
	m.cacheMu.Unlock()

	data, err := os.ReadFile(path)
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

func fileModTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
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
