package marketapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMigratesLegacyJSONToCommentedINI(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "market_config.json")
	if err := os.WriteFile(legacy, []byte(`{"listen_addr":"127.0.0.1:9000","restock":{"equipment_inflate_min":3,"equipment_inflate_max":9,"upgrade_min":8,"upgrade_max":12,"upgrade_price_rate":0.1,"rand_low":0.8,"rand_high":1.2}}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, path, status, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "market_config.ini") || status.MigratedFrom != legacy {
		t.Fatalf("path=%q status=%+v", path, status)
	}
	if cfg.Restock.EquipInflateMin != 3 || cfg.Restock.UpgradeMin != 8 || cfg.Restock.RandLow != 0.8 {
		t.Fatalf("legacy values were not migrated: %+v", cfg.Restock)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[auction_price]") || !strings.Contains(text, "# 装备基础价格的最小随机倍率。") {
		t.Fatalf("generated INI lacks documented pricing configuration:\n%s", text)
	}
	for _, unused := range []string{"listen_addr", "frida_db", "[service]", "auto_sync", "nexon_base", "recycle_price"} {
		if strings.Contains(text, unused) {
			t.Fatalf("generated INI still contains unused setting %q:\n%s", unused, text)
		}
	}
	if strings.Count(text, "max_result_actions =") != 1 || strings.Count(text, "per_item_delay_ms =") != 1 {
		t.Fatalf("collector duplicated restock-only action detail settings:\n%s", text)
	}
}

func TestLoadConfigRecoversInvalidLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "market_config.json")
	if err := os.WriteFile(legacy, []byte("{broken config"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, path, status, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Recovered || status.BackupPath != legacy+".invalid" || cfg.Restock.RandLow != DefaultConfig().Restock.RandLow {
		t.Fatalf("status=%+v cfg=%+v", status, cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigNormalizesINIAndKeepsExplicitSwitches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "market_config.ini")
	raw := `[auction_price]
equipment_level_min = 40
equipment_level_max = 70
equip_inflate_min = 4
equip_inflate_max = 7
upgrade_min = 6
upgrade_max = 11
upgrade_price_rate = 0.12
rand_low = 0.75
rand_high = 1.25
custom_price_enabled = true
custom_price_file = prices.json

[auction_collect]
enabled = false
price_range_enabled = true
in_range_probability = 0.9
out_of_range_probability = 0.02
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, _, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restock.EquipmentLevelMin != 40 || cfg.Restock.EquipmentLevelMax != 70 || cfg.Restock.EquipInflateMin != 4 || cfg.Restock.UpgradePriceRate != 0.12 || !cfg.Restock.CustomPriceEnabled {
		t.Fatalf("pricing config=%+v", cfg.Restock)
	}
	if cfg.Collector.Enabled || !cfg.Collector.PriceRangeEnabled || cfg.Collector.InRangeProbability != 0.9 || cfg.Collector.OutRangeProbability != 0.02 {
		t.Fatalf("collector config=%+v", cfg.Collector)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, comment := range []string{
		"# 单轮补货最多生成并执行的动作数；0 表示配置层不限制。",
		"# 补货动作的最大并发工作数。",
		"# 单个任务结果中最多保留的动作明细数，避免接口和日志数据过大。",
		"# 同一工作线程连续执行补货或回收动作时的间隔毫秒数；0 表示不主动等待。",
		"# 允许上架的最低装备等级；0 表示不限制最低等级。",
		"# 允许上架的最高装备等级；0 表示不限制最高等级。",
	} {
		if !strings.Contains(text, comment) {
			t.Fatalf("normalized INI lacks action-limit comment %q:\n%s", comment, text)
		}
	}
}
