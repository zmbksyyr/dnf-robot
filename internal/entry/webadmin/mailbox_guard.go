package webadmin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type mailboxGuardConfig struct {
	Enabled bool `json:"mailbox_bad_node_guard"`
}

type mailboxGuardRequest struct {
	Enabled bool `json:"mailbox_bad_node_guard"`
}

type mailboxGuardStatus struct {
	DesiredEnabled bool   `json:"desired_enabled"`
	Enabled        bool   `json:"enabled"`
	State          string `json:"state"`
	PID            int    `json:"pid,omitempty"`
	Port           int    `json:"port"`
	Message        string `json:"message,omitempty"`
}

func (s *Server) handleCompat(w http.ResponseWriter, r *http.Request) {
	// Serialize all df_game_r compatibility inspection and patch operations.
	s.partyCompatMu.Lock()
	defer s.partyCompatMu.Unlock()

	cfg, err := s.loadMailboxGuardConfig()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		status := inspectMailboxGuard(s.cfg.RobotGamePort)
		status.DesiredEnabled = cfg.Enabled
		writeJSON(w, map[string]interface{}{"ok": true, "result": status})
	case http.MethodPost:
		var req mailboxGuardRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		cfg.Enabled = req.Enabled
		if err := s.saveMailboxGuardConfig(cfg); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		status := inspectMailboxGuard(s.cfg.RobotGamePort)
		status.DesiredEnabled = cfg.Enabled
		status.Message = "saved; restart Robot to apply"
		writeJSON(w, map[string]interface{}{
			"ok":               true,
			"restart_required": true,
			"message":          "Saved; restart Robot to apply",
			"result":           status,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) mailboxGuardConfigPath() string {
	return filepath.Join(s.cfg.ConfigDir, "compat.json")
}

func (s *Server) loadMailboxGuardConfig() (mailboxGuardConfig, error) {
	cfg := mailboxGuardConfig{}
	data, err := os.ReadFile(s.mailboxGuardConfigPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("read compatibility config: %w", err)
	}
	return cfg, nil
}

func (s *Server) saveMailboxGuardConfig(cfg mailboxGuardConfig) error {
	if err := os.MkdirAll(s.cfg.ConfigDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.mailboxGuardConfigPath()
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
