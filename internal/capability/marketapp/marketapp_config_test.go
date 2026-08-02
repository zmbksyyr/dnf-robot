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
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.Restock.EquipInflateMin != 3 || cfg.Restock.UpgradeMin != 8 || cfg.Restock.RandLow != 0.8 {
		t.Fatalf("legacy values were not migrated: %+v", cfg.Restock)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[auction_price]") || !strings.Contains(text, "# 装备基础价格的随机倍率范围。") {
		t.Fatalf("generated INI lacks documented pricing configuration:\n%s", text)
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
	if cfg.Restock.EquipInflateMin != 4 || cfg.Restock.UpgradePriceRate != 0.12 || !cfg.Restock.CustomPriceEnabled {
		t.Fatalf("pricing config=%+v", cfg.Restock)
	}
	if cfg.Collector.Enabled || !cfg.Collector.PriceRangeEnabled || cfg.Collector.InRangeProbability != 0.9 || cfg.Collector.OutRangeProbability != 0.02 {
		t.Fatalf("collector config=%+v", cfg.Collector)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# 概率范围为 0.0 到 1.0") {
		t.Fatalf("normalized INI lacks comments:\n%s", data)
	}
}
