package marketapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRecoversInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "market_config.json")
	invalid := []byte("{broken config")
	if err := os.WriteFile(path, invalid, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, gotPath, status, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if !status.Recovered || status.BackupPath != path+".invalid" || status.Reason == "" {
		t.Fatalf("status = %+v", status)
	}
	if cfg.ListenAddr != DefaultConfig().ListenAddr || cfg.Auto.Enabled {
		t.Fatalf("fallback config = %+v", cfg)
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(restored) {
		t.Fatalf("restored config is invalid: %q", restored)
	}
	backup, err := os.ReadFile(status.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(invalid) {
		t.Fatalf("invalid backup = %q, want %q", backup, invalid)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary config remains: %v", err)
	}
}

func TestLoadConfigRewritesValidConfigAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "market_config.json")
	if err := os.WriteFile(path, []byte(`{"listen_addr":"127.0.0.1:9000"}`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, status, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if status.Recovered {
		t.Fatalf("valid config reported fallback: %+v", status)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.AuctionPort != DefaultConfig().AuctionPort {
		t.Fatalf("config defaults were not applied: %+v", cfg)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary config remains: %v", err)
	}
}
