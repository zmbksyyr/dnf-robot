package webadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleGamePort(w http.ResponseWriter, _ *http.Request) {
	addr := net.JoinHostPort(s.cfg.RobotConnectIP, strconv.Itoa(s.cfg.RobotGamePort))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "addr": addr, "error": err.Error()})
		return
	}
	_ = conn.Close()
	out := map[string]interface{}{"ok": true, "addr": addr}
	if maxUser, cfgName, cfgPath, ok := s.gameMaxUserNum(); ok {
		out["max_user_num"] = maxUser
		out["game_cfg_name"] = cfgName
		out["game_cfg_path"] = cfgPath
	}
	writeJSON(w, out)
}

func (s *Server) handleMaxUser(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		maxUser, files, err := s.readMaxUserNum()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "max_user_num": maxUser, "files": files, "running": dfGameRRunning()})
	case http.MethodPost:
		var req struct {
			MaxUserNum int `json:"max_user_num"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if req.MaxUserNum <= 0 || req.MaxUserNum > 1000000 {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "max_user_num must be between 1 and 1000000"})
			return
		}
		files, err := s.writeMaxUserNum(req.MaxUserNum)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		running := dfGameRRunning()
		out := map[string]interface{}{"ok": true, "max_user_num": req.MaxUserNum, "files": files, "running": running}
		if running {
			out["message"] = "max_user_num updated; df_game_r is running, restart df_game_r for the change to take effect"
		} else {
			out["message"] = "max_user_num updated"
		}
		writeJSON(w, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleServerScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	script := ""
	switch strings.TrimSpace(req.Action) {
	case "run":
		script = "/root/run"
	case "stop":
		script = "/root/stop"
	default:
		writeJSON(w, map[string]interface{}{"ok": false, "error": "unknown script action"})
		return
	}
	info, err := os.Stat(script)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "script": script, "error": err.Error()})
		return
	}
	if info.IsDir() {
		writeJSON(w, map[string]interface{}{"ok": false, "script": script, "error": "script path is a directory"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", script)
	cmd.Dir = "/root"
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		writeJSON(w, map[string]interface{}{"ok": false, "script": script, "output": string(out), "error": "script timed out"})
		return
	}
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "script": script, "output": string(out), "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "script": script, "output": string(out)})
}

func (s *Server) handleMonitorService(w http.ResponseWriter, _ *http.Request) {
	s.handleLocalTCPService(w, s.cfg.MonitorPort)
}

func (s *Server) handleRelayService(w http.ResponseWriter, _ *http.Request) {
	s.handleLocalTCPService(w, s.cfg.RelayPort)
}

func (s *Server) handleLocalTCPService(w http.ResponseWriter, port int) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "addr": addr, "state": "closed", "error": err.Error()})
		return
	}
	_ = conn.Close()
	writeJSON(w, map[string]interface{}{"ok": true, "addr": addr, "state": "open"})
}

func (s *Server) readMaxUserNum() (int, []string, error) {
	paths, err := s.gameCfgPaths()
	if err != nil {
		return 0, nil, err
	}
	re := regexp.MustCompile(`(?m)^\s*max_user_num\s*=\s*([0-9]+)\s*$`)
	files := make([]string, 0, len(paths))
	maxUser := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, nil, err
		}
		m := re.FindSubmatch(data)
		if len(m) != 2 {
			continue
		}
		n, err := strconv.Atoi(string(m[1]))
		if err != nil || n <= 0 {
			continue
		}
		files = append(files, path)
		if maxUser == 0 {
			maxUser = n
		}
	}
	if len(files) == 0 {
		return 0, nil, fmt.Errorf("no max_user_num found under %s", filepath.Join(filepath.Dir(s.cfg.DFGameR), "cfg"))
	}
	return maxUser, files, nil
}

func (s *Server) writeMaxUserNum(maxUser int) ([]string, error) {
	paths, err := s.gameCfgPaths()
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(?m)^(\s*max_user_num\s*=\s*)[0-9]+(\s*)$`)
	updated := make([]string, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !re.Match(data) {
			continue
		}
		next := re.ReplaceAll(data, []byte("${1}"+strconv.Itoa(maxUser)+"${2}"))
		if err := os.WriteFile(path, next, 0644); err != nil {
			return nil, err
		}
		updated = append(updated, path)
	}
	if len(updated) == 0 {
		return nil, fmt.Errorf("no max_user_num found under %s", filepath.Join(filepath.Dir(s.cfg.DFGameR), "cfg"))
	}
	return updated, nil
}

func (s *Server) gameCfgPaths() ([]string, error) {
	if s.cfg == nil || strings.TrimSpace(s.cfg.DFGameR) == "" {
		return nil, fmt.Errorf("DfGameR is not configured")
	}
	cfgDir := filepath.Join(filepath.Dir(s.cfg.DFGameR), "cfg")
	paths, err := filepath.Glob(filepath.Join(cfgDir, "*.cfg"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no cfg files found under %s", cfgDir)
	}
	return paths, nil
}

func dfGameRRunning() bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		for _, part := range parts {
			base := filepath.Base(part)
			if base == "df_game_r" || strings.HasSuffix(part, "/df_game_r") {
				return true
			}
		}
	}
	return false
}

func (s *Server) gameMaxUserNum() (int, string, string, bool) {
	cfgName, ok := gameConfigNameForPort(s.cfg.RobotGamePort)
	if !ok || cfgName == "" || strings.ContainsAny(cfgName, `/\`) {
		return 0, "", "", false
	}
	cfgPath := filepath.Join(filepath.Dir(s.cfg.DFGameR), "cfg", cfgName+".cfg")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return 0, "", "", false
	}
	re := regexp.MustCompile(`(?m)^\s*max_user_num\s*=\s*([0-9]+)\s*$`)
	m := re.FindSubmatch(data)
	if len(m) != 2 {
		return 0, "", "", false
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil || n <= 0 {
		return 0, "", "", false
	}
	return n, cfgName, cfgPath, true
}

func gameConfigNameForPort(port int) (string, bool) {
	cmd := exec.Command("ss", "-lntp")
	data, err := cmd.Output()
	if err != nil || len(data) == 0 {
		return "", false
	}
	portPattern := regexp.MustCompile(`:` + regexp.QuoteMeta(strconv.Itoa(port)) + `\s`)
	var line []byte
	for _, candidate := range bytes.Split(data, []byte{'\n'}) {
		if portPattern.Match(candidate) {
			line = candidate
			break
		}
	}
	if len(line) == 0 {
		return "", false
	}
	re := regexp.MustCompile(`pid=([0-9]+)`)
	m := re.FindSubmatch(line)
	if len(m) != 2 {
		return "", false
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", string(m[1]), "cmdline"))
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	for i, part := range parts {
		if strings.HasSuffix(part, "df_game_r") || part == "df_game_r" || strings.Contains(part, "/df_game_r") {
			if i+1 < len(parts) && parts[i+1] != "" {
				return parts[i+1], true
			}
		}
	}
	return "", false
}
