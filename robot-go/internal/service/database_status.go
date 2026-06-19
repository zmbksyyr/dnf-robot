package service

import (
	"context"
	"fmt"
	"time"
)

type DatabaseStatus struct {
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

func (m *RobotManager) DatabaseStatus() DatabaseStatus {
	st := DatabaseStatus{CheckedAt: time.Now()}
	if m.cfg != nil {
		st.Host = m.cfg.DBHost
		st.Port = m.cfg.DBPort
		st.Database = m.cfg.DBName
		st.User = m.cfg.DBUser
		st.Target = fmt.Sprintf("%s:%d/%s as %s", m.cfg.DBHost, m.cfg.DBPort, m.cfg.DBName, m.cfg.DBUser)
	}
	if m.db == nil {
		st.Error = "database pool is nil"
		return st
	}
	stats := m.db.Stats()
	st.OpenConns = stats.OpenConnections
	st.InUse = stats.InUse
	st.Idle = stats.Idle

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := m.db.PingContext(ctx); err != nil {
		st.LatencyMS = time.Since(start).Milliseconds()
		st.Error = err.Error()
		return st
	}
	var one int
	if err := m.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		st.LatencyMS = time.Since(start).Milliseconds()
		st.Error = err.Error()
		return st
	}
	st.LatencyMS = time.Since(start).Milliseconds()
	st.SelectVerified = one == 1
	st.OK = st.SelectVerified
	return st
}
