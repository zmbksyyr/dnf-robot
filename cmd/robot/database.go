package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"

	"robot/internal/foundation/config"
)

const (
	databasePingTimeout = 5 * time.Second
	databaseMaxIdleTime = 5 * time.Minute
)

func openDatabase(cfg *config.SysConfig) (*sql.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil database config")
	}
	mysqlCfg := mysqlDatabaseConfig(cfg)
	database, err := sql.Open("mysql", mysqlCfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(cfg.DBMaxSize)
	database.SetMaxIdleConns(cfg.DBInitSize)
	database.SetConnMaxIdleTime(databaseMaxIdleTime)
	database.SetConnMaxLifetime(time.Duration(cfg.DBConnMaxLifetimeSec) * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), databasePingTimeout)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return database, nil
}

func mysqlDatabaseConfig(cfg *config.SysConfig) *mysql.Config {
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = cfg.DBUser
	mysqlCfg.Passwd = cfg.DBPassword
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(cfg.DBHost, strconv.Itoa(cfg.DBPort))
	mysqlCfg.DBName = cfg.DBName
	// The original game uses zero dates for unset timestamps. Initialize this
	// session setting on every pooled connection, including reconnects, while
	// preserving strict validation and the server's global configuration.
	mysqlCfg.Params = map[string]string{
		"charset":  "utf8",
		"sql_mode": "TRIM(BOTH ',' FROM REPLACE(REPLACE(CONCAT(',',@@SESSION.sql_mode,','),',NO_ZERO_DATE,',','),',NO_ZERO_IN_DATE,',','))",
	}
	mysqlCfg.ParseTime = true
	mysqlCfg.Timeout = time.Duration(cfg.DBDialTimeoutSec) * time.Second
	mysqlCfg.ReadTimeout = time.Duration(cfg.DBReadTimeoutSec) * time.Second
	mysqlCfg.WriteTimeout = time.Duration(cfg.DBWriteTimeoutSec) * time.Second
	return mysqlCfg
}
