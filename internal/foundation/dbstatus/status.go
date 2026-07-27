package dbstatus

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"robot/internal/foundation/config"
)

type Database interface {
	Stats() sql.DBStats
	PingContext(ctx context.Context) error
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type Status struct {
	OK             bool      `json:"ok"`
	Host           string    `json:"host,omitempty"`
	Port           int       `json:"port,omitempty"`
	Database       string    `json:"database,omitempty"`
	User           string    `json:"user,omitempty"`
	Target         string    `json:"target,omitempty"`
	OpenConns      int       `json:"open_conns"`
	InUse          int       `json:"in_use"`
	Idle           int       `json:"idle"`
	Error          string    `json:"error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
	LatencyMS      int64     `json:"latency_ms"`
	SelectVerified bool      `json:"select_verified"`
}

func Check(db Database, cfg *config.SysConfig) Status {
	st := Status{CheckedAt: time.Now()}
	if cfg != nil {
		st.Host = cfg.DBHost
		st.Port = cfg.DBPort
		st.Database = cfg.DBName
		st.User = cfg.DBUser
		st.Target = fmt.Sprintf("%s:%d/%s as %s", cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBUser)
	}
	if db == nil {
		st.Error = "database pool is nil"
		return st
	}
	stats := db.Stats()
	st.OpenConns = stats.OpenConnections
	st.InUse = stats.InUse
	st.Idle = stats.Idle

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := db.PingContext(ctx); err != nil {
		st.LatencyMS = time.Since(start).Milliseconds()
		st.Error = err.Error()
		return st
	}
	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		st.LatencyMS = time.Since(start).Milliseconds()
		st.Error = err.Error()
		return st
	}
	st.LatencyMS = time.Since(start).Milliseconds()
	st.SelectVerified = one == 1
	st.OK = st.SelectVerified
	return st
}

type TableRequirement struct {
	Schema  string
	Table   string
	Columns []string
}

type SchemaCheck struct {
	Schema string `json:"schema"`
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
}

type TableCheck struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Exists  bool     `json:"exists"`
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type StructureReport struct {
	Target  string        `json:"target"`
	Connect Status        `json:"connect"`
	Schemas []SchemaCheck `json:"schemas"`
	Tables  []TableCheck  `json:"tables"`
}

func CheckStructure(cfg *config.SysConfig, schemas []string, tables []TableRequirement) StructureReport {
	report := StructureReport{}
	if cfg == nil {
		report.Connect = Status{CheckedAt: time.Now(), Error: "config is nil"}
		return report
	}
	report.Target = fmt.Sprintf("%s:%d/%s as %s", cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBUser)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8&timeout=5s&readTimeout=5s&writeTimeout=5s&parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		report.Connect = Status{Host: cfg.DBHost, Port: cfg.DBPort, Database: cfg.DBName, User: cfg.DBUser, Target: report.Target, CheckedAt: time.Now(), Error: err.Error()}
		return report
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	report.Connect = Check(db, cfg)
	if !report.Connect.OK {
		return report
	}
	for _, schema := range schemas {
		ok, err := schemaExists(db, schema)
		entry := SchemaCheck{Schema: schema, Exists: ok}
		if err != nil {
			entry.Error = err.Error()
		}
		report.Schemas = append(report.Schemas, entry)
	}
	for _, req := range tables {
		exists, missing, err := tableHasColumns(db, req.Schema, req.Table, req.Columns)
		entry := TableCheck{Schema: req.Schema, Table: req.Table, Exists: exists, Missing: missing}
		if err != nil {
			entry.Error = err.Error()
		}
		report.Tables = append(report.Tables, entry)
	}
	return report
}

func schemaExists(db *sql.DB, schema string) (bool, error) {
	var got string
	err := db.QueryRow("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=? LIMIT 1", schema).Scan(&got)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func tableHasColumns(db *sql.DB, schema, table string, columns []string) (bool, []string, error) {
	rows, err := db.Query("SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=?", schema, table)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return false, nil, err
		}
		have[col] = true
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	if len(have) == 0 {
		return false, columns, nil
	}
	missing := make([]string, 0)
	for _, col := range columns {
		if !have[col] {
			missing = append(missing, col)
		}
	}
	return len(missing) == 0, missing, nil
}
