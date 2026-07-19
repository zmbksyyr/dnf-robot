package webadmin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"robot/internal/capability/robotconfig"
)

const (
	defaultPartyCompatAccountStart uint32 = 17000000
	defaultPartyCompatAccountEnd   uint32 = 17001000
	partyCompatDefaultAccountLimit uint64 = 1000
)

type partyCompatConfig struct {
	Enabled      bool   `json:"enabled"`
	AccountStart uint32 `json:"account_start"`
	AccountEnd   uint32 `json:"account_end"`
}

type partyCompatStatus struct {
	DesiredEnabled     bool   `json:"desired_enabled"`
	Enabled            bool   `json:"enabled"`
	State              string `json:"state"`
	PID                int    `json:"pid,omitempty"`
	Port               int    `json:"port"`
	AccountStart       uint32 `json:"account_start"`
	AccountEnd         uint32 `json:"account_end"`
	Message            string `json:"message,omitempty"`
	FailCount          int    `json:"fail_count,omitempty"`
	NextRetrySec       int    `json:"next_retry_sec,omitempty"`
	processUnavailable bool
	orphanedCave       bool
}

type partyCompatRequest struct {
	Action       string `json:"action"`
	AccountStart uint32 `json:"account_start"`
	AccountEnd   uint32 `json:"account_end"`
}

func (s *Server) handlePartyCompat(w http.ResponseWriter, r *http.Request) {
	s.partyCompatMu.Lock()
	defer s.partyCompatMu.Unlock()

	cfg, err := s.loadPartyCompatConfig()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		status := s.inspectPartyCompatLocked(cfg)
		writeJSON(w, map[string]interface{}{"ok": true, "result": status})
	case http.MethodPost:
		var req partyCompatRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if err := validatePartyCompatRange(req.AccountStart, req.AccountEnd); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		enable := false
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "on":
			enable = true
		case "off":
		default:
			writeJSON(w, map[string]interface{}{"ok": false, "error": "action must be on or off"})
			return
		}
		cfg = partyCompatConfig{Enabled: enable, AccountStart: req.AccountStart, AccountEnd: req.AccountEnd}
		if err := s.savePartyCompatConfig(cfg); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		s.resetPartyCompatFailuresLocked()
		s.wakePartyCompatSupervisor()
		status, applyErr := setPartyCompat(s.cfg.RobotGamePort, cfg, enable)
		status.DesiredEnabled = cfg.Enabled
		if applyErr != nil {
			status = s.inspectPartyCompatLocked(cfg)
			status.Message = "desired state saved; apply pending: " + applyErr.Error()
		} else {
			status.Message = "desired state saved and applied"
		}
		writeJSON(w, map[string]interface{}{"ok": true, "result": status})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) partyCompatConfigPath() string {
	return filepath.Join(s.cfg.ConfigDir, "party_compat.json")
}

func (s *Server) loadPartyCompatConfig() (partyCompatConfig, error) {
	cfg := s.defaultPartyCompatConfig()
	data, err := os.ReadFile(s.partyCompatConfigPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var raw struct {
		Enabled      *bool  `json:"enabled"`
		AccountStart uint32 `json:"account_start"`
		AccountEnd   uint32 `json:"account_end"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("read party compatibility config: %w", err)
	}
	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	if raw.AccountStart != 0 {
		cfg.AccountStart = raw.AccountStart
	}
	if raw.AccountEnd != 0 {
		cfg.AccountEnd = raw.AccountEnd
	}
	if err := validatePartyCompatRange(cfg.AccountStart, cfg.AccountEnd); err != nil {
		return cfg, fmt.Errorf("read party compatibility config: %w", err)
	}
	return cfg, nil
}

func (s *Server) defaultPartyCompatConfig() partyCompatConfig {
	cfg := partyCompatConfig{Enabled: true, AccountStart: defaultPartyCompatAccountStart, AccountEnd: defaultPartyCompatAccountEnd}
	if s == nil || s.cfg == nil || s.cfg.ConfigDir == "" {
		return cfg
	}
	runtimeConfig, err := robotconfig.LoadFile(filepath.Join(s.cfg.ConfigDir, "robot_config.ini"))
	if err != nil || runtimeConfig.RobotUIDStart <= 0 || runtimeConfig.RobotUIDEnd < runtimeConfig.RobotUIDStart || uint64(runtimeConfig.RobotUIDEnd) >= uint64(^uint32(0)) {
		return cfg
	}
	start, end, ok := partyCompatConfiguredWindow(runtimeConfig.RobotUIDStart, runtimeConfig.RobotUIDEnd)
	if !ok {
		return cfg
	}
	cfg.AccountStart = start
	cfg.AccountEnd = end
	return cfg
}

func partyCompatConfiguredWindow(start, end int) (uint32, uint32, bool) {
	if start <= 0 || end < start || uint64(end) >= uint64(^uint32(0)) {
		return 0, 0, false
	}
	exclusiveEnd := uint64(end) + 1
	limitEnd := uint64(start) + partyCompatDefaultAccountLimit
	if exclusiveEnd > limitEnd {
		exclusiveEnd = limitEnd
	}
	if exclusiveEnd > uint64(^uint32(0)) {
		return 0, 0, false
	}
	return uint32(start), uint32(exclusiveEnd), true
}

func (s *Server) savePartyCompatConfig(cfg partyCompatConfig) error {
	if err := os.MkdirAll(s.cfg.ConfigDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.partyCompatConfigPath()
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

func (s *Server) inspectPartyCompatLocked(cfg partyCompatConfig) partyCompatStatus {
	status := inspectPartyCompat(s.cfg.RobotGamePort, cfg)
	status.DesiredEnabled = cfg.Enabled
	status.FailCount = s.partyCompatFailures
	if !s.partyCompatNextRetry.IsZero() {
		next := int(time.Until(s.partyCompatNextRetry).Round(time.Second) / time.Second)
		if next > 0 {
			status.NextRetrySec = next
		}
	}
	if status.Message == "" && s.partyCompatLastError != "" {
		status.Message = s.partyCompatLastError
	}
	if cfg.Enabled && status.processUnavailable {
		status.Message = partyCompatWaitingMessage(status.Message)
	}
	return status
}

func validatePartyCompatRange(start, end uint32) error {
	if start == 0 || end == 0 || start >= end {
		return fmt.Errorf("account range must be positive and start must be less than end")
	}
	return nil
}
