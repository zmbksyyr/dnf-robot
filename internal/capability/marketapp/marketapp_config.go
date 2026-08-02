package marketapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	foundationconfig "robot/internal/foundation/config"
)

const defaultMarketMaxActions = 10000

func DefaultConfig() Config {
	return Config{
		ListenAddr: "0.0.0.0:8121", FridaDB: "frida", GameDB: "taiwan_cain_2nd",
		AuctionDB: "taiwan_cain_auction_gold", CeraDB: "taiwan_cain_auction_cera",
		AuctionHost: "127.0.0.1", AuctionPort: 30803, CeraHost: "127.0.0.1", CeraPort: 30603,
		ItemInfoSourcePath: "pvf_iteminfo.dat",
		ItemInfoTargets:    []string{"/home/neople/auction/iteminfo.dat", "/home/neople/point/iteminfo.dat", "/home/dxf/auction/iteminfo.dat", "/home/dxf/point/iteminfo.dat"},
		SystemOwner:        SystemOwner{IDBase: 90000001, BuyerBase: 90100001, NexonBase: 18000000, OwnerName: "market", CeraName: "gold", RotateEvery: 10},
		Collector:          CollectorCfg{Enabled: true, MaxConcurrent: 8, MaxResultActions: 200, InRangeProbability: 0.8, OutRangeProbability: 0.05},
		Restock: RestockCfg{
			Comments: defaultRestockComments(), QualityFilter: boolPtr(true), StackSizes: []int{500, 1000, 2000},
			EquipmentQtyMin: 2, EquipmentQtyMax: 5, EquipInflateMin: 5, EquipInflateMax: 8,
			UpgradeMin: 7, UpgradeMax: 13, UpgradePriceRate: 0.08, RandLow: 0.9, RandHigh: 1.1,
			CustomPriceFile: "market_item_price_ranges.json", MaxActions: defaultMarketMaxActions, MaxConcurrent: 8, MaxResultActions: 200,
		},
		Cera: CeraCfg{Comments: defaultCeraComments(), Items: defaultCeraRows()},
		Auto: AutoCfg{Markets: []string{marketNameAuction, marketNameCera}, InitialDelayMS: 3000, IntervalMS: 60000, MaxActions: defaultMarketMaxActions, MaxConcurrent: 8, ContinueOnError: true},
	}
}

func LoadConfig(configDir string) (Config, string, error) {
	cfg, path, _, err := loadConfig(configDir)
	return cfg, path, err
}

type configLoadStatus struct {
	Recovered    bool
	BackupPath   string
	Reason       string
	MigratedFrom string
}

func loadConfig(configDir string) (Config, string, configLoadStatus, error) {
	path := filepath.Join(configDir, "market_config.ini")
	legacyPath := filepath.Join(configDir, "market_config.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
			cfg, status, loadErr := loadLegacyMarketJSON(legacyPath)
			if loadErr != nil {
				return cfg, path, status, loadErr
			}
			if err := writeMarketConfig(path, cfg); err != nil {
				return cfg, path, status, err
			}
			status.MigratedFrom = legacyPath
			return cfg, path, status, nil
		}
		cfg := DefaultConfig()
		if err := writeMarketConfig(path, cfg); err != nil {
			return cfg, path, configLoadStatus{}, err
		}
		return cfg, path, configLoadStatus{}, nil
	}
	ini, err := foundationconfig.Load(path)
	if err != nil {
		return DefaultConfig(), path, configLoadStatus{}, err
	}
	cfg := decodeMarketINI(ini)
	cfg.applyDefaults()
	if err := writeMarketConfig(path, cfg); err != nil {
		return cfg, path, configLoadStatus{}, err
	}
	return cfg, path, configLoadStatus{}, nil
}

func loadLegacyMarketJSON(path string) (Config, configLoadStatus, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, configLoadStatus{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		backup := path + ".invalid"
		if writeErr := os.WriteFile(backup, data, 0644); writeErr != nil {
			return cfg, configLoadStatus{}, fmt.Errorf("decode legacy market config: %v; backup: %w", err, writeErr)
		}
		return cfg, configLoadStatus{Recovered: true, BackupPath: backup, Reason: err.Error()}, nil
	}
	cfg.applyDefaults()
	return cfg, configLoadStatus{}, nil
}

func decodeMarketINI(ini *foundationconfig.INIConfig) Config {
	d := DefaultConfig()
	c := d
	c.ListenAddr = ini.GetString("market", "listen_addr", d.ListenAddr)
	c.FridaDB = ini.GetString("database", "frida_db", d.FridaDB)
	c.GameDB = ini.GetString("database", "game_db", d.GameDB)
	c.AuctionDB = ini.GetString("database", "auction_db", d.AuctionDB)
	c.CeraDB = ini.GetString("database", "cera_db", d.CeraDB)
	c.AuctionHost = ini.GetString("service", "auction_host", d.AuctionHost)
	c.AuctionPort = ini.GetInt("service", "auction_port", d.AuctionPort)
	c.CeraHost = ini.GetString("service", "cera_host", d.CeraHost)
	c.CeraPort = ini.GetInt("service", "cera_port", d.CeraPort)
	c.ItemInfoSourcePath = ini.GetString("iteminfo", "source_path", d.ItemInfoSourcePath)
	c.ItemInfoTargets = splitStrings(ini.GetString("iteminfo", "targets", strings.Join(d.ItemInfoTargets, ",")))
	c.AutoSyncItemInfo = iniBool(ini, "iteminfo", "auto_sync", d.AutoSyncItemInfo)
	c.SystemOwner.IDBase = uint32(ini.GetInt("system_owner", "id_base", int(d.SystemOwner.IDBase)))
	c.SystemOwner.BuyerBase = uint32(ini.GetInt("system_owner", "buyer_base", int(d.SystemOwner.BuyerBase)))
	c.SystemOwner.NexonBase = uint32(ini.GetInt("system_owner", "nexon_base", int(d.SystemOwner.NexonBase)))
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
	c.Collector.MaxResultActions = ini.GetInt("auction_collect", "max_result_actions", d.Collector.MaxResultActions)
	c.Collector.PerItemDelayMS = ini.GetInt("auction_collect", "per_item_delay_ms", d.Collector.PerItemDelayMS)
	c.Restock.QualityFilter = boolPtr(iniBool(ini, "auction_price", "quality_filter", true))
	c.Restock.StackSizes = splitInts(ini.GetString("auction_price", "stack_sizes", joinInts(d.Restock.StackSizes)))
	c.Restock.EquipmentQtyMin = ini.GetInt("auction_price", "equipment_qty_min", d.Restock.EquipmentQtyMin)
	c.Restock.EquipmentQtyMax = ini.GetInt("auction_price", "equipment_qty_max", d.Restock.EquipmentQtyMax)
	c.Restock.EquipInflateMin = ini.GetInt("auction_price", "equip_inflate_min", d.Restock.EquipInflateMin)
	c.Restock.EquipInflateMax = ini.GetInt("auction_price", "equip_inflate_max", d.Restock.EquipInflateMax)
	c.Restock.UpgradeMin = ini.GetInt("auction_price", "upgrade_min", d.Restock.UpgradeMin)
	c.Restock.UpgradeMax = ini.GetInt("auction_price", "upgrade_max", d.Restock.UpgradeMax)
	c.Restock.UpgradePriceRate = iniFloat(ini, "auction_price", "upgrade_price_rate", d.Restock.UpgradePriceRate)
	c.Restock.RandLow = iniFloat(ini, "auction_price", "rand_low", d.Restock.RandLow)
	c.Restock.RandHigh = iniFloat(ini, "auction_price", "rand_high", d.Restock.RandHigh)
	c.Restock.CustomPriceEnabled = iniBool(ini, "auction_price", "custom_price_enabled", d.Restock.CustomPriceEnabled)
	c.Restock.CustomPriceFile = ini.GetString("auction_price", "custom_price_file", d.Restock.CustomPriceFile)
	c.Restock.MaxActions = ini.GetInt("auction_price", "max_actions", d.Restock.MaxActions)
	c.Restock.MaxConcurrent = ini.GetInt("auction_price", "max_concurrent", d.Restock.MaxConcurrent)
	c.Restock.MaxResultActions = ini.GetInt("auction_price", "max_result_actions", d.Restock.MaxResultActions)
	c.Restock.PerItemDelayMS = ini.GetInt("auction_price", "per_item_delay_ms", d.Restock.PerItemDelayMS)
	c.Cera.Items = decodeCeraItems(ini.GetString("cera", "items", encodeCeraItems(d.Cera.Items)), d.Cera.Items)
	c.Auto.Enabled = iniBool(ini, "auto", "enabled", d.Auto.Enabled)
	c.Auto.Markets = splitStrings(ini.GetString("auto", "markets", strings.Join(d.Auto.Markets, ",")))
	c.Auto.InitialDelayMS = ini.GetInt("auto", "initial_delay_ms", d.Auto.InitialDelayMS)
	c.Auto.IntervalMS = ini.GetInt("auto", "interval_ms", d.Auto.IntervalMS)
	c.Auto.MaxActions = ini.GetInt("auto", "max_actions", d.Auto.MaxActions)
	c.Auto.MaxConcurrent = ini.GetInt("auto", "max_concurrent", d.Auto.MaxConcurrent)
	c.Auto.ContinueOnError = iniBool(ini, "auto", "continue_on_error", d.Auto.ContinueOnError)
	return c
}

func (c *Config) applyDefaults() {
	d := DefaultConfig()
	if c.ListenAddr == "" {
		c.ListenAddr = d.ListenAddr
	}
	if c.FridaDB == "" {
		c.FridaDB = d.FridaDB
	}
	if c.GameDB == "" {
		c.GameDB = d.GameDB
	}
	if c.AuctionDB == "" {
		c.AuctionDB = d.AuctionDB
	}
	if c.CeraDB == "" {
		c.CeraDB = d.CeraDB
	}
	if c.AuctionHost == "" {
		c.AuctionHost = d.AuctionHost
	}
	if c.AuctionPort <= 0 {
		c.AuctionPort = d.AuctionPort
	}
	if c.CeraHost == "" {
		c.CeraHost = d.CeraHost
	}
	if c.CeraPort <= 0 {
		c.CeraPort = d.CeraPort
	}
	if c.ItemInfoSourcePath == "" {
		c.ItemInfoSourcePath = d.ItemInfoSourcePath
	}
	if len(c.ItemInfoTargets) == 0 {
		c.ItemInfoTargets = d.ItemInfoTargets
	}
	if c.SystemOwner.IDBase == 0 {
		c.SystemOwner.IDBase = d.SystemOwner.IDBase
	}
	if c.SystemOwner.BuyerBase == 0 {
		c.SystemOwner.BuyerBase = d.SystemOwner.BuyerBase
	}
	if c.SystemOwner.NexonBase == 0 {
		c.SystemOwner.NexonBase = d.SystemOwner.NexonBase
	}
	if c.SystemOwner.OwnerName == "" {
		c.SystemOwner.OwnerName = d.SystemOwner.OwnerName
	}
	if c.SystemOwner.CeraName == "" {
		c.SystemOwner.CeraName = d.SystemOwner.CeraName
	}
	if c.SystemOwner.RotateEvery <= 0 {
		c.SystemOwner.RotateEvery = d.SystemOwner.RotateEvery
	}
	if c.Collector.MaxConcurrent <= 0 {
		c.Collector.MaxConcurrent = d.Collector.MaxConcurrent
	}
	if c.Collector.MaxResultActions <= 0 {
		c.Collector.MaxResultActions = d.Collector.MaxResultActions
	}
	if c.Collector.PerItemDelayMS < 0 {
		c.Collector.PerItemDelayMS = 0
	}
	c.Collector.InRangeProbability = clampProbability(c.Collector.InRangeProbability, d.Collector.InRangeProbability)
	c.Collector.OutRangeProbability = clampProbability(c.Collector.OutRangeProbability, d.Collector.OutRangeProbability)
	mergeStringMap(&c.Restock.Comments, d.Restock.Comments)
	if c.Restock.QualityFilter == nil {
		c.Restock.QualityFilter = boolPtr(true)
	}
	if len(c.Restock.StackSizes) == 0 {
		c.Restock.StackSizes = d.Restock.StackSizes
	}
	if c.Restock.EquipmentQtyMin <= 0 {
		c.Restock.EquipmentQtyMin = d.Restock.EquipmentQtyMin
	}
	if c.Restock.EquipmentQtyMax < c.Restock.EquipmentQtyMin {
		c.Restock.EquipmentQtyMax = c.Restock.EquipmentQtyMin
	}
	if c.Restock.EquipInflateMin <= 0 {
		c.Restock.EquipInflateMin = d.Restock.EquipInflateMin
	}
	if c.Restock.EquipInflateMax < c.Restock.EquipInflateMin {
		c.Restock.EquipInflateMax = c.Restock.EquipInflateMin
	}
	if c.Restock.UpgradeMin < 0 {
		c.Restock.UpgradeMin = d.Restock.UpgradeMin
	}
	if c.Restock.UpgradeMax < c.Restock.UpgradeMin {
		c.Restock.UpgradeMax = c.Restock.UpgradeMin
	}
	if c.Restock.UpgradePriceRate < 0 {
		c.Restock.UpgradePriceRate = d.Restock.UpgradePriceRate
	}
	if c.Restock.RandLow <= 0 {
		c.Restock.RandLow = d.Restock.RandLow
	}
	if c.Restock.RandHigh <= 0 {
		c.Restock.RandHigh = d.Restock.RandHigh
	}
	if c.Restock.RandHigh < c.Restock.RandLow {
		c.Restock.RandHigh = c.Restock.RandLow
	}
	if c.Restock.CustomPriceFile == "" {
		c.Restock.CustomPriceFile = d.Restock.CustomPriceFile
	}
	if c.Restock.MaxConcurrent <= 0 {
		c.Restock.MaxConcurrent = d.Restock.MaxConcurrent
	}
	if c.Restock.MaxResultActions <= 0 {
		c.Restock.MaxResultActions = d.Restock.MaxResultActions
	}
	if c.Restock.PerItemDelayMS < 0 {
		c.Restock.PerItemDelayMS = 0
	}
	if len(c.Cera.Items) == 0 {
		c.Cera.Items = d.Cera.Items
	}
	for i := range c.Cera.Items {
		if c.Cera.Items[i].ItemID == 2675347 && c.Cera.Items[i].Label == "3000w_gold" {
			c.Cera.Items[i].Enabled = true
		}
	}
	mergeStringMap(&c.Cera.Comments, d.Cera.Comments)
	if len(c.Auto.Markets) == 0 {
		c.Auto.Markets = d.Auto.Markets
	}
	if c.Auto.InitialDelayMS < 0 {
		c.Auto.InitialDelayMS = d.Auto.InitialDelayMS
	}
	if c.Auto.IntervalMS < 60000 {
		c.Auto.IntervalMS = d.Auto.IntervalMS
	}
	if c.Auto.MaxConcurrent <= 0 {
		c.Auto.MaxConcurrent = d.Auto.MaxConcurrent
	}
}

func writeMarketConfig(path string, c Config) error {
	c.applyDefaults()
	qf := c.Restock.QualityFilter == nil || *c.Restock.QualityFilter
	lines := []string{
		"# Market configuration. The program rewrites this file with normalized values and comments.",
		"[market]", "# Market TCP listen address.", "listen_addr = " + c.ListenAddr, "",
		"[database]", "# Database names used by Market.", "frida_db = " + c.FridaDB, "game_db = " + c.GameDB, "auction_db = " + c.AuctionDB, "cera_db = " + c.CeraDB, "",
		"[service]", "# Native Auction and Point service endpoints.", fmt.Sprintf("auction_host = %s", c.AuctionHost), fmt.Sprintf("auction_port = %d", c.AuctionPort), fmt.Sprintf("cera_host = %s", c.CeraHost), fmt.Sprintf("cera_port = %d", c.CeraPort), "",
		"[iteminfo]", "# Source and comma-separated release targets for iteminfo.dat.", "source_path = " + c.ItemInfoSourcePath, "targets = " + strings.Join(c.ItemInfoTargets, ","), "auto_sync = " + strconv.FormatBool(c.AutoSyncItemInfo), "",
		"[system_owner]", "# Virtual seller and buyer ID ranges. rotate_every controls how many records use one ID.", fmt.Sprintf("id_base = %d", c.SystemOwner.IDBase), fmt.Sprintf("buyer_base = %d", c.SystemOwner.BuyerBase), fmt.Sprintf("nexon_base = %d", c.SystemOwner.NexonBase), "owner_name = " + c.SystemOwner.OwnerName, "cera_name = " + c.SystemOwner.CeraName, fmt.Sprintf("rotate_every = %d", c.SystemOwner.RotateEvery), "",
		"[auction_price]",
		"# 是否过滤不适合自动补货的高稀有度物品。", "quality_filter = " + strconv.FormatBool(qf),
		"# 堆叠物品的候选数量，使用逗号分隔；实际数量不会超过 PVF stack_limit。", "stack_sizes = " + joinInts(c.Restock.StackSizes),
		"# 每种缺货装备生成的拍卖记录数量范围。", fmt.Sprintf("equipment_qty_min = %d", c.Restock.EquipmentQtyMin), fmt.Sprintf("equipment_qty_max = %d", c.Restock.EquipmentQtyMax),
		"# 装备基础价格的随机倍率范围。", fmt.Sprintf("equip_inflate_min = %d", c.Restock.EquipInflateMin), fmt.Sprintf("equip_inflate_max = %d", c.Restock.EquipInflateMax),
		"# 装备随机强化范围以及每级强化的价格加成比例；0.08 表示每级增加 8%。", fmt.Sprintf("upgrade_min = %d", c.Restock.UpgradeMin), fmt.Sprintf("upgrade_max = %d", c.Restock.UpgradeMax), "upgrade_price_rate = " + formatFloat(c.Restock.UpgradePriceRate),
		"# 装备倍率和强化加价计算完成后应用的最终随机价格倍率。", "rand_low = " + formatFloat(c.Restock.RandLow), "rand_high = " + formatFloat(c.Restock.RandHigh),
		"# 是否启用物品独立最终价格范围；有效的单品配置优先于上面的通用公式。", "custom_price_enabled = " + strconv.FormatBool(c.Restock.CustomPriceEnabled),
		"# 单品价格 JSON 文件；相对路径以配置目录为基准，也可以填写绝对路径。", "custom_price_file = " + c.Restock.CustomPriceFile,
		"# 补货执行限制；max_actions=0 表示配置层不限制本轮动作数。", fmt.Sprintf("max_actions = %d", c.Restock.MaxActions), fmt.Sprintf("max_concurrent = %d", c.Restock.MaxConcurrent), fmt.Sprintf("max_result_actions = %d", c.Restock.MaxResultActions), fmt.Sprintf("per_item_delay_ms = %d", c.Restock.PerItemDelayMS), "",
		"[auction_collect]", "# 虚拟买家自动回收总开关。", "enabled = " + strconv.FormatBool(c.Collector.Enabled),
		"# 手动回收时是否包含系统虚拟卖家的订单，通常保持 false。", "include_system_owners = " + strconv.FormatBool(c.Collector.IncludeSystemOwners),
		"# 开启后，价格符合最终范围的订单使用高概率，超出范围的订单使用低概率。", "price_range_enabled = " + strconv.FormatBool(c.Collector.PriceRangeEnabled),
		"# 概率范围为 0.0 到 1.0；0.8 表示约 80%。", "in_range_probability = " + formatFloat(c.Collector.InRangeProbability), "out_of_range_probability = " + formatFloat(c.Collector.OutRangeProbability),
		"# 回收执行限制；max_actions=0 表示配置层不限制本轮动作数。", fmt.Sprintf("max_actions = %d", c.Collector.MaxActions), fmt.Sprintf("max_concurrent = %d", c.Collector.MaxConcurrent), fmt.Sprintf("max_result_actions = %d", c.Collector.MaxResultActions), fmt.Sprintf("per_item_delay_ms = %d", c.Collector.PerItemDelayMS), "",
		"[cera]", "# Gold-consignment rows: item_id|name|restock_price|target_records|recycle_price|enabled, separated by commas.", "items = " + encodeCeraItems(c.Cera.Items), "",
		"[auto]", "# Periodic Market automation. interval_ms has a minimum of 60000.", "enabled = " + strconv.FormatBool(c.Auto.Enabled), "markets = " + strings.Join(c.Auto.Markets, ","), fmt.Sprintf("initial_delay_ms = %d", c.Auto.InitialDelayMS), fmt.Sprintf("interval_ms = %d", c.Auto.IntervalMS), fmt.Sprintf("max_actions = %d", c.Auto.MaxActions), fmt.Sprintf("max_concurrent = %d", c.Auto.MaxConcurrent), "continue_on_error = " + strconv.FormatBool(c.Auto.ContinueOnError), "",
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
func clampProbability(v, fallback float64) float64 {
	if v < 0 || v > 1 {
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
		out = append(out, fmt.Sprintf("%d|%s|%d|%d|%d|%t", row.ItemID, strings.ReplaceAll(row.Label, "|", " "), row.RestockPrice, row.RestockQty, row.RecyclePrice, row.Enabled))
	}
	return strings.Join(out, ",")
}

func decodeCeraItems(raw string, fallback []ceraRow) []ceraRow {
	var out []ceraRow
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.Split(strings.TrimSpace(entry), "|")
		if len(parts) != 6 {
			continue
		}
		id, e1 := strconv.ParseUint(parts[0], 10, 32)
		restock, e2 := strconv.ParseInt(parts[2], 10, 32)
		qty, e3 := strconv.Atoi(parts[3])
		recycle, e4 := strconv.ParseInt(parts[4], 10, 32)
		enabled, e5 := strconv.ParseBool(parts[5])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || id == 0 {
			continue
		}
		out = append(out, ceraRow{ItemID: uint32(id), Label: parts[1], RestockPrice: int32(restock), RestockQty: qty, RecyclePrice: int32(recycle), Enabled: enabled})
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
