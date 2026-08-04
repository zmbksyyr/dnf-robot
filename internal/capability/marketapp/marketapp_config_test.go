package marketapp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"robot/internal/foundation/config"
	"robot/internal/foundation/layout"
)

func TestNewRejectsEmptyConfigDir(t *testing.T) {
	if _, err := New(&sql.DB{}, &config.SysConfig{}, nil); err == nil || !strings.Contains(err.Error(), "config dir") {
		t.Fatalf("New error = %v, want empty config dir", err)
	}
}

func TestLoadConfigCreatesCommentedINIInConfDirectory(t *testing.T) {
	dir := t.TempDir()
	_, path, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != layout.New(dir).MarketConfig() {
		t.Fatalf("path=%q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[auction_price]") || !strings.Contains(text, "# 装备基础价格的最小随机倍率。") {
		t.Fatalf("generated INI lacks documented pricing configuration:\n%s", text)
	}
	if !strings.Contains(text, "allowed_rarities = 01234") || strings.Contains(text, "quality_filter =") {
		t.Fatalf("generated INI does not use the default listed rarity digits:\n%s", text)
	}
	for _, unused := range []string{"listen_addr", "frida_db", "[service]", "auto_sync", "nexon_base", "recycle_price", "market_config.json", "custom_price_file", "source_path"} {
		if strings.Contains(text, unused) {
			t.Fatalf("generated INI still contains unused setting %q:\n%s", unused, text)
		}
	}
	if strings.Count(text, "max_result_actions =") != 1 || strings.Count(text, "per_item_delay_ms =") != 1 {
		t.Fatalf("collector duplicated restock-only action detail settings:\n%s", text)
	}
}

func TestLoadConfigNormalizesAllowedRaritiesAndMigratesLegacySettings(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[auction_price]\nallowed_rarities = 43004\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restock.AllowedRarities != "034" {
		t.Fatalf("allowed rarities=%q, want 034", cfg.Restock.AllowedRarities)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "allowed_rarities = 034") {
		t.Fatalf("normalized config does not contain canonical allowed rarities:\n%s", data)
	}

	if err := os.WriteFile(path, []byte("[auction_price]\nblocked_rarities = 97550\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restock.AllowedRarities != "123468" {
		t.Fatalf("legacy blocked rarities migrated to %q, want 123468", cfg.Restock.AllowedRarities)
	}

	if err := os.WriteFile(path, []byte("[auction_price]\nquality_filter = false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restock.AllowedRarities != "0123456789" {
		t.Fatalf("legacy disabled filter migrated to %q, want all digits", cfg.Restock.AllowedRarities)
	}
}

func TestLoadConfigRejectsInvalidAllowedRarities(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[auction_price]\nallowed_rarities = 01a4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "digits 0..9") {
		t.Fatalf("invalid allowed rarities error=%v", err)
	}
}

func TestLoadConfigRejectsInvalidCurrentINI(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken line without section"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(dir); err == nil {
		t.Fatal("invalid market INI unexpectedly loaded")
	}
}

func TestLoadConfigNormalizesINIAndKeepsExplicitSwitches(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
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

[auction_collect]
enabled = false
price_range_enabled = true
in_range_probability = 0.9
out_of_range_probability = 0.02
`
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(dir)
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

func TestLoadConfigRejectsRemovedDynamicPaths(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{
		"[iteminfo]\nsource_path = ../pvf/iteminfo.dat\n",
		"[auction_price]\ncustom_price_file = prices.json\n",
	} {
		if err := os.WriteFile(path, []byte(setting), 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadConfig(dir); err == nil {
			t.Fatalf("removed dynamic path unexpectedly accepted: %s", setting)
		}
	}
}

func TestLoadConfigRejectsUnknownAndDuplicateSettings(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"[unknown]\nvalue = 1\n",
		"[auto]\nenabeld = true\n",
		"[auto]\nenabled = true\nenabled = false\n",
	} {
		if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadConfig(dir); err == nil {
			t.Fatalf("invalid market setting unexpectedly accepted: %s", raw)
		}
	}
}

func TestLoadConfigRejectsNonCanonicalCase(t *testing.T) {
	dir := t.TempDir()
	path := layout.New(dir).MarketConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"[DATABASE]\ngame_db = custom_game\n",
		"[database]\nGAME_DB = custom_game\n",
	} {
		if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "canonical lowercase") {
			t.Fatalf("non-canonical market setting error = %v, raw=%q", err, raw)
		}
	}
}
