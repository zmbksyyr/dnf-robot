package marketapp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"robot/internal/foundation/atomicfile"
	foundationconfig "robot/internal/foundation/config"
	"robot/internal/foundation/layout"
)

const (
	defaultMarketMaxActions  = 10000
	defaultEquipmentRarities = "012345"
	defaultOtherRarities     = "012345"
	tradePolicyPermissive    = "permissive"
	tradePolicyStrict        = "strict"
)

func DefaultConfig() Config {
	return Config{
		GameDB:    "taiwan_cain_2nd",
		AuctionDB: "taiwan_cain_auction_gold", CeraDB: "taiwan_cain_auction_cera",
		AuctionHost: "127.0.0.1", AuctionPort: 30803, CeraHost: "127.0.0.1", CeraPort: 30603,
		ItemInfoTargets: []string{"/home/neople/auction/iteminfo.dat", "/home/neople/point/iteminfo.dat", "/home/dxf/auction/iteminfo.dat", "/home/dxf/point/iteminfo.dat"},
		SystemOwner:     SystemOwner{IDBase: 90000001, BuyerBase: 90100001, OwnerName: "market", CeraName: "gold", RotateEvery: 10},
		Collector:       CollectorCfg{Enabled: true, MaxConcurrent: 8, InRangeProbability: 0.8, OutRangeProbability: 0.05},
		Restock: RestockCfg{
			Comments: defaultRestockComments(), EquipmentAllowedRarities: defaultEquipmentRarities, OtherAllowedRarities: defaultOtherRarities,
			EquipmentTradePolicy: tradePolicyPermissive, OtherTradePolicy: tradePolicyPermissive, StackSizes: []int{500, 1000, 2000},
			EquipmentQtyMin: 2, EquipmentQtyMax: 5, EquipInflateMin: 5, EquipInflateMax: 8,
			UpgradeMin: 7, UpgradeMax: 13, UpgradePriceRate: 0.08, RandLow: 0.9, RandHigh: 1.1,
			MaxActions: defaultMarketMaxActions, MaxConcurrent: 8, MaxResultActions: 200,
		},
		Cera: CeraCfg{Comments: defaultCeraComments(), Items: defaultCeraRows()},
		Auto: AutoCfg{Markets: []string{marketNameAuction, marketNameCera}, InitialDelayMS: 3000, IntervalMS: 60000, MaxActions: defaultMarketMaxActions, MaxConcurrent: 8, ContinueOnError: true},
	}
}

func LoadConfig(configDir string) (Config, string, error) {
	return loadConfig(configDir)
}

func loadConfig(configDir string) (Config, string, error) {
	path := layout.New(configDir).MarketConfig()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := writeMarketConfig(path, cfg); err != nil {
			return cfg, path, err
		}
		return cfg, path, nil
	}
	cfg, err := loadConfigSnapshot(path)
	if err != nil {
		return DefaultConfig(), path, err
	}
	if err := writeMarketConfig(path, cfg); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}

func decodeMarketINI(ini *foundationconfig.INIConfig) (Config, error) {
	d := DefaultConfig()
	c := d
	c.GameDB = ini.GetString("database", "game_db", d.GameDB)
	c.AuctionDB = ini.GetString("database", "auction_db", d.AuctionDB)
	c.CeraDB = ini.GetString("database", "cera_db", d.CeraDB)
	c.ItemInfoTargets = splitStrings(ini.GetString("iteminfo", "targets", strings.Join(d.ItemInfoTargets, ",")))
	c.SystemOwner.IDBase = uint32(ini.GetInt("system_owner", "id_base", int(d.SystemOwner.IDBase)))
	c.SystemOwner.BuyerBase = uint32(ini.GetInt("system_owner", "buyer_base", int(d.SystemOwner.BuyerBase)))
	c.SystemOwner.OwnerName = ini.GetString("system_owner", "owner_name", d.SystemOwner.OwnerName)
	c.SystemOwner.CeraName = ini.GetString("system_owner", "cera_name", d.SystemOwner.CeraName)
	c.SystemOwner.RotateEvery = ini.GetInt("system_owner", "rotate_every", d.SystemOwner.RotateEvery)
	c.Collector.Enabled = iniBool(ini, "auction_collect", "enabled", d.Collector.Enabled)
	c.Collector.IncludeSystemOwners = iniBool(ini, "auction_collect", "include_system_owners", d.Collector.IncludeSystemOwners)
	c.Collector.PriceRangeEnabled = iniBool(ini, "auction_collect", "price_range_enabled", d.Collector.PriceRangeEnabled)
	c.Collector.InRangeProbability = iniFloat(ini, "auction_collect", "in_range_probability", d.Collector.InRangeProbability)
	c.Collector.OutRangeProbability = iniFloat(ini, "auction_collect", "out_of_range_probability", d.Collector.OutRangeProbability)
	c.Collector.MaxActions = ini.GetInt("auction_collect", "max_actions", d.Collector.MaxActions)
	c.Collector.MaxConcurrent = ini.GetInt("auction_collect", "max_concurrent", d.Collector.MaxConcurrent)
	var err error
	c.Restock.EquipmentAllowedRarities, err = normalizeAllowedRarities(ini.GetString("auction_price", "equipment_allowed_rarities", defaultEquipmentRarities))
	if err != nil {
		return Config{}, err
	}
	c.Restock.OtherAllowedRarities, err = normalizeAllowedRarities(ini.GetString("auction_price", "other_allowed_rarities", defaultOtherRarities))
	if err != nil {
		return Config{}, err
	}
	c.Restock.EquipmentTradePolicy = ini.GetString("auction_price", "equipment_trade_policy", d.Restock.EquipmentTradePolicy)
	c.Restock.OtherTradePolicy = ini.GetString("auction_price", "other_trade_policy", d.Restock.OtherTradePolicy)
	c.Restock.BlockedItemIDs, err = decodeBlockedItemIDs(ini.GetString("auction_price", "blocked_item_ids", ""))
	if err != nil {
		return Config{}, err
	}
	c.Restock.StackSizes = splitInts(ini.GetString("auction_price", "stack_sizes", joinInts(d.Restock.StackSizes)))
	c.Restock.EquipmentQtyMin = ini.GetInt("auction_price", "equipment_qty_min", d.Restock.EquipmentQtyMin)
	c.Restock.EquipmentQtyMax = ini.GetInt("auction_price", "equipment_qty_max", d.Restock.EquipmentQtyMax)
	c.Restock.EquipmentLevelMin = ini.GetInt("auction_price", "equipment_level_min", d.Restock.EquipmentLevelMin)
	c.Restock.EquipmentLevelMax = ini.GetInt("auction_price", "equipment_level_max", d.Restock.EquipmentLevelMax)
	c.Restock.EquipInflateMin = ini.GetInt("auction_price", "equip_inflate_min", d.Restock.EquipInflateMin)
	c.Restock.EquipInflateMax = ini.GetInt("auction_price", "equip_inflate_max", d.Restock.EquipInflateMax)
	c.Restock.UpgradeMin = ini.GetInt("auction_price", "upgrade_min", d.Restock.UpgradeMin)
	c.Restock.UpgradeMax = ini.GetInt("auction_price", "upgrade_max", d.Restock.UpgradeMax)
	c.Restock.UpgradePriceRate = iniFloat(ini, "auction_price", "upgrade_price_rate", d.Restock.UpgradePriceRate)
	c.Restock.RandLow = iniFloat(ini, "auction_price", "rand_low", d.Restock.RandLow)
	c.Restock.RandHigh = iniFloat(ini, "auction_price", "rand_high", d.Restock.RandHigh)
	c.Restock.CustomPriceEnabled = iniBool(ini, "auction_price", "custom_price_enabled", d.Restock.CustomPriceEnabled)
	c.Restock.MaxActions = ini.GetInt("auction_price", "max_actions", d.Restock.MaxActions)
	c.Restock.MaxConcurrent = ini.GetInt("auction_price", "max_concurrent", d.Restock.MaxConcurrent)
	c.Restock.MaxResultActions = ini.GetInt("auction_price", "max_result_actions", d.Restock.MaxResultActions)
	c.Restock.PerItemDelayMS = ini.GetInt("auction_price", "per_item_delay_ms", d.Restock.PerItemDelayMS)
	c.Cera.Items, err = decodeCeraItems(ini.GetString("cera", "items", encodeCeraItems(d.Cera.Items)))
	if err != nil {
		return Config{}, err
	}
	c.Auto.Enabled = iniBool(ini, "auto", "enabled", d.Auto.Enabled)
	c.Auto.Markets = splitStrings(ini.GetString("auto", "markets", strings.Join(d.Auto.Markets, ",")))
	c.Auto.InitialDelayMS = ini.GetInt("auto", "initial_delay_ms", d.Auto.InitialDelayMS)
	c.Auto.IntervalMS = ini.GetInt("auto", "interval_ms", d.Auto.IntervalMS)
	c.Auto.MaxActions = ini.GetInt("auto", "max_actions", d.Auto.MaxActions)
	c.Auto.MaxConcurrent = ini.GetInt("auto", "max_concurrent", d.Auto.MaxConcurrent)
	c.Auto.ContinueOnError = iniBool(ini, "auto", "continue_on_error", d.Auto.ContinueOnError)
	return c, nil
}

func writeMarketConfig(path string, c Config) error {
	if err := validateMarketConfig(c); err != nil {
		return err
	}
	lines := []string{
		"# Market 配置。程序只在整份配置完整校验通过后加载，并按规范格式重写本文件。",
		"[database]",
		"# 普通拍卖行数据库。", "auction_db = " + c.AuctionDB,
		"# 金币寄售数据库。", "cera_db = " + c.CeraDB,
		"# 游戏数据库，用于宠物实例和系统特殊物品清理。", "game_db = " + c.GameDB, "",
		"[iteminfo]",
		"# 源文件固定为 pvf/iteminfo.dat；这里只配置发布目标，多个路径使用逗号分隔。", "targets = " + strings.Join(c.ItemInfoTargets, ","), "",
		"[system_owner]",
		"# 系统虚拟卖家的起始角色 ID。", fmt.Sprintf("id_base = %d", c.SystemOwner.IDBase),
		"# 系统虚拟买家的起始角色 ID。", fmt.Sprintf("buyer_base = %d", c.SystemOwner.BuyerBase),
		"# 普通拍卖行虚拟卖家和买家的名称。", "owner_name = " + c.SystemOwner.OwnerName,
		"# 金币寄售虚拟卖家的名称。", "cera_name = " + c.SystemOwner.CeraName,
		"# 每个虚拟角色连续使用多少条订单后切换到下一个 ID。", fmt.Sprintf("rotate_every = %d", c.SystemOwner.RotateEvery), "",
		"[auction_price]",
		"# 允许自动补货上架的装备稀有度数字，只允许 0 到 9；缺少稀有度的数据按 0 处理。", "equipment_allowed_rarities = " + c.Restock.EquipmentAllowedRarities,
		"# 允许自动补货上架的其他物品稀有度数字，只允许 0 到 9。", "other_allowed_rarities = " + c.Restock.OtherAllowedRarities,
		"# 装备交易策略：permissive 或 strict。", "equipment_trade_policy = " + c.Restock.EquipmentTradePolicy,
		"# 其他物品交易策略：permissive 或 strict。", "other_trade_policy = " + c.Restock.OtherTradePolicy,
		"# 禁止自动或指定补货上架的物品 ID，使用逗号分隔；保存设置重建后会回收已有机器人库存。", "blocked_item_ids = " + joinUint32s(c.Restock.BlockedItemIDs),
		"# 堆叠物品的候选数量，使用逗号分隔；实际数量不会超过 PVF stack_limit。", "stack_sizes = " + joinInts(c.Restock.StackSizes),
		"# 每种缺货装备最少生成的拍卖记录数。", fmt.Sprintf("equipment_qty_min = %d", c.Restock.EquipmentQtyMin),
		"# 每种缺货装备最多生成的拍卖记录数。", fmt.Sprintf("equipment_qty_max = %d", c.Restock.EquipmentQtyMax),
		"# 允许上架的最低装备等级；0 表示不限制最低等级。", fmt.Sprintf("equipment_level_min = %d", c.Restock.EquipmentLevelMin),
		"# 允许上架的最高装备等级；0 表示不限制最高等级。", fmt.Sprintf("equipment_level_max = %d", c.Restock.EquipmentLevelMax),
		"# 装备基础价格的最小随机倍率。", fmt.Sprintf("equip_inflate_min = %d", c.Restock.EquipInflateMin),
		"# 装备基础价格的最大随机倍率。", fmt.Sprintf("equip_inflate_max = %d", c.Restock.EquipInflateMax),
		"# 装备随机强化的最低等级。", fmt.Sprintf("upgrade_min = %d", c.Restock.UpgradeMin),
		"# 装备随机强化的最高等级。", fmt.Sprintf("upgrade_max = %d", c.Restock.UpgradeMax),
		"# 每级强化的价格加成比例；0.08 表示每级增加 8%。", "upgrade_price_rate = " + formatFloat(c.Restock.UpgradePriceRate),
		"# 最终价格的最小随机倍率。", "rand_low = " + formatFloat(c.Restock.RandLow),
		"# 最终价格的最大随机倍率。", "rand_high = " + formatFloat(c.Restock.RandHigh),
		"# 是否启用 conf/market_item_price_ranges.json 中的物品独立最终价格范围；有效配置优先于上面的通用公式。", "custom_price_enabled = " + strconv.FormatBool(c.Restock.CustomPriceEnabled),
		"# 单轮补货最多生成并执行的动作数；0 表示配置层不限制。", fmt.Sprintf("max_actions = %d", c.Restock.MaxActions),
		"# 补货动作的最大并发工作数。", fmt.Sprintf("max_concurrent = %d", c.Restock.MaxConcurrent),
		"# 单个任务结果中最多保留的动作明细数，避免接口和日志数据过大。", fmt.Sprintf("max_result_actions = %d", c.Restock.MaxResultActions),
		"# 同一工作线程连续执行补货或回收动作时的间隔毫秒数；0 表示不主动等待。", fmt.Sprintf("per_item_delay_ms = %d", c.Restock.PerItemDelayMS), "",
		"[auction_collect]",
		"# 虚拟买家自动回收总开关。", "enabled = " + strconv.FormatBool(c.Collector.Enabled),
		"# 手动回收时是否包含系统虚拟卖家的订单，通常保持 false。", "include_system_owners = " + strconv.FormatBool(c.Collector.IncludeSystemOwners),
		"# 开启后，价格符合最终范围的订单使用高概率，超出范围的订单使用低概率。", "price_range_enabled = " + strconv.FormatBool(c.Collector.PriceRangeEnabled),
		"# 价格在最终范围内时的回收概率，范围为 0.0 到 1.0。", "in_range_probability = " + formatFloat(c.Collector.InRangeProbability),
		"# 价格超出最终范围时的回收概率，范围为 0.0 到 1.0。", "out_of_range_probability = " + formatFloat(c.Collector.OutRangeProbability),
		"# 单轮回收最多生成并执行的动作数；0 表示配置层不限制。", fmt.Sprintf("max_actions = %d", c.Collector.MaxActions),
		"# 回收动作的最大并发工作数。", fmt.Sprintf("max_concurrent = %d", c.Collector.MaxConcurrent), "",
		"[cera]",
		"# 金币寄售条目，多个条目使用逗号分隔。",
		"# 单条格式：物品ID|备注名称|寄售价格|目标记录数|是否启用。", "items = " + encodeCeraItems(c.Cera.Items), "",
		"[auto]",
		"# 是否启用周期性 Market 自动任务。", "enabled = " + strconv.FormatBool(c.Auto.Enabled),
		"# 自动处理的市场，支持 auction 和 cera，多个值使用逗号分隔。", "markets = " + strings.Join(c.Auto.Markets, ","),
		"# 程序启动后首次执行自动任务前的等待毫秒数。", fmt.Sprintf("initial_delay_ms = %d", c.Auto.InitialDelayMS),
		"# 自动任务执行间隔毫秒数，最小为 60000。", fmt.Sprintf("interval_ms = %d", c.Auto.IntervalMS),
		"# 自动任务每个市场单轮允许的最大动作数；0 表示不限制。", fmt.Sprintf("max_actions = %d", c.Auto.MaxActions),
		"# 自动任务每个市场允许的最大并发工作数。", fmt.Sprintf("max_concurrent = %d", c.Auto.MaxConcurrent),
		"# 单个动作失败后是否继续执行本轮剩余动作。", "continue_on_error = " + strconv.FormatBool(c.Auto.ContinueOnError), "",
	}
	return writeAtomicFile(path, []byte(strings.Join(lines, "\n")))
}

func writeJSONFile(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, append(data, '\n'))
}

func writeAtomicFile(path string, data []byte) error {
	return atomicfile.WriteFile(path, data, 0644)
}

func boolPtr(v bool) *bool         { return &v }
func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
func iniBool(c *foundationconfig.INIConfig, section, key string, fallback bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(c.GetString(section, key, strconv.FormatBool(fallback))))
	if err != nil {
		return fallback
	}
	return v
}
func iniFloat(c *foundationconfig.INIConfig, section, key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(c.GetString(section, key, formatFloat(fallback))), 64)
	if err != nil {
		return fallback
	}
	return v
}
func splitStrings(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func splitInts(v string) []int {
	var out []int
	for _, s := range splitStrings(v) {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}
func joinInts(v []int) string {
	out := make([]string, 0, len(v))
	for _, n := range v {
		out = append(out, strconv.Itoa(n))
	}
	return strings.Join(out, ",")
}

func decodeBlockedItemIDs(raw string) ([]uint32, error) {
	values := splitStrings(raw)
	out := make([]uint32, 0, len(values))
	seen := make(map[uint32]struct{}, len(values))
	for _, value := range values {
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || parsed == 0 {
			return nil, fmt.Errorf("auction_price.blocked_item_ids contains invalid item ID %q", value)
		}
		id := uint32(parsed)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func normalizeBlockedItemIDs(values []uint32) []uint32 {
	out := append([]uint32(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	write := 0
	for _, value := range out {
		if value == 0 || write > 0 && out[write-1] == value {
			continue
		}
		out[write] = value
		write++
	}
	return out[:write]
}

func joinUint32s(values []uint32) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatUint(uint64(value), 10))
	}
	return strings.Join(out, ",")
}
func mergeStringMap(dst *map[string]string, defaults map[string]string) {
	if *dst == nil {
		*dst = map[string]string{}
	}
	for k, v := range defaults {
		if _, ok := (*dst)[k]; !ok {
			(*dst)[k] = v
		}
	}
}

func encodeCeraItems(items []ceraRow) string {
	out := make([]string, 0, len(items))
	for _, row := range items {
		out = append(out, fmt.Sprintf("%d|%s|%d|%d|%t", row.ItemID, strings.ReplaceAll(row.Label, "|", " "), row.RestockPrice, row.RestockQty, row.Enabled))
	}
	return strings.Join(out, ",")
}

func decodeCeraItems(raw string) ([]ceraRow, error) {
	entries := strings.Split(raw, ",")
	out := make([]ceraRow, 0, len(entries))
	for index, entry := range entries {
		parts := strings.Split(strings.TrimSpace(entry), "|")
		if len(parts) != 5 {
			return nil, fmt.Errorf("cera item %d must contain exactly 5 fields", index+1)
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		id, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("cera item %d has invalid item ID", index+1)
		}
		restock, err := strconv.ParseInt(parts[2], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cera item %d has invalid restock price", index+1)
		}
		qty, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("cera item %d has invalid restock quantity", index+1)
		}
		if parts[4] != "true" && parts[4] != "false" {
			return nil, fmt.Errorf("cera item %d enabled must be true or false", index+1)
		}
		enabled := parts[4] == "true"
		out = append(out, ceraRow{ItemID: uint32(id), Label: parts[1], RestockPrice: int32(restock), RestockQty: qty, Enabled: enabled})
	}
	if err := validateCeraItems(out); err != nil {
		return nil, err
	}
	return out, nil
}
