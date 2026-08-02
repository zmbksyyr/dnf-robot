package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigUsesIndependentDatabaseTimeouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	data := []byte("[db]\n" +
		"db_dial_timeout_sec = 8\n" +
		"db_read_timeout_sec = 45\n" +
		"db_write_timeout_sec = 60\n" +
		"db_conn_max_lifetime_sec = 3600\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBDialTimeoutSec != 8 || cfg.DBReadTimeoutSec != 45 || cfg.DBWriteTimeoutSec != 60 || cfg.DBConnMaxLifetimeSec != 3600 {
		t.Fatalf("database timeouts not loaded independently: %+v", cfg)
	}
}

func TestLoadConfigRejectsOutOfRangeDatabaseTimeouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	data := []byte("[db]\n" +
		"db_dial_timeout_sec = 300\n" +
		"db_read_timeout_sec = 300\n" +
		"db_write_timeout_sec = -1\n" +
		"db_conn_max_lifetime_sec = 999999\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("out-of-range database timeouts unexpectedly loaded")
	}
}
