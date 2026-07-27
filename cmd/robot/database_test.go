package main

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"robot/internal/foundation/config"
)

func TestDatabaseDSNUsesIndependentTimeouts(t *testing.T) {
	cfg := &config.SysConfig{
		DBHost: "127.0.0.1", DBPort: 3306, DBName: "d_taiwan",
		DBUser: "game", DBPassword: "u^u!5%",
		DBDialTimeoutSec: 5, DBReadTimeoutSec: 30, DBWriteTimeoutSec: 40,
	}
	parsed, err := mysql.ParseDSN(mysqlDatabaseConfig(cfg).FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Passwd != cfg.DBPassword || parsed.Timeout != 5*time.Second || parsed.ReadTimeout != 30*time.Second || parsed.WriteTimeout != 40*time.Second {
		t.Fatalf("database config changed settings: %+v", parsed)
	}
}
