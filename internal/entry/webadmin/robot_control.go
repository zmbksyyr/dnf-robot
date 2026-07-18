package webadmin

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"robot/internal/capability/robotconfig"
	"robot/internal/foundation/config"
	"runtime"
	"strconv"
	"strings"
)

func (s *Server) handleGameEndpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.loadDiskConfig()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, s.gameEndpointPayload(cfg, ""))
	case http.MethodPost:
		var req struct {
			GamePort int `json:"game_port"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if req.GamePort <= 0 || req.GamePort > 65535 {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "game_port must be between 1 and 65535"})
			return
		}
		cfg, err := s.writeGamePort(req.GamePort)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, s.gameEndpointPayload(cfg, "saved; restart robot to apply"))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRestartRobot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if err := startRobotRestartHelper(exe, s.cfg.ConfigDir); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "robot restart queued", "exe": exe})
}

func (s *Server) gameEndpointPayload(cfg *config.SysConfig, message string) map[string]interface{} {
	connectIP := ""
	gamePort := 0
	addr := ""
	if cfg != nil {
		connectIP = cfg.RobotConnectIP
		gamePort = cfg.RobotGamePort
		addr = net.JoinHostPort(connectIP, strconv.Itoa(gamePort))
	}
	out := map[string]interface{}{
		"ok":          true,
		"connect_ip":  connectIP,
		"game_port":   gamePort,
		"addr":        addr,
		"config_path": s.configPath(),
	}
	if message != "" {
		out["message"] = message
	}
	return out
}

func (s *Server) loadDiskConfig() (*config.SysConfig, error) {
	cfg, err := config.LoadConfig(s.configPath())
	if err != nil {
		return nil, err
	}
	cfg.ConfigDir = s.cfg.ConfigDir
	return cfg, nil
}

func (s *Server) writeGamePort(port int) (*config.SysConfig, error) {
	path := s.configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := robotconfig.UpdateINIText(string(data), map[string]string{
		"Robot.RobotGamePort": strconv.Itoa(port),
	})
	if _, err := config.LoadFromString(text); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return nil, err
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	cfg.ConfigDir = s.cfg.ConfigDir
	s.cfg.RobotGamePort = cfg.RobotGamePort
	s.cfg.RobotConnectIP = cfg.RobotConnectIP
	return cfg, nil
}

func (s *Server) configPath() string {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.ConfigDir) == "" {
		return filepath.Join("config", "config.ini")
	}
	return filepath.Join(s.cfg.ConfigDir, "config.ini")
}

func startRobotRestartHelper(exe, configDir string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("restart robot is only supported on linux")
	}
	if strings.TrimSpace(exe) == "" {
		return fmt.Errorf("empty executable path")
	}
	logPath := filepath.Join(configDir, "robot_stdout.log")
	workDir := filepath.Dir(exe)
	script := fmt.Sprintf(`(
sleep 1
exe=%s
for d in /proc/[0-9]*; do
  pid=${d#/proc/}
  target=$(readlink "$d/exe" 2>/dev/null || true)
  if [ "$target" = "$exe" ]; then
    kill -TERM "$pid" 2>/dev/null || true
  fi
done
sleep 2
for d in /proc/[0-9]*; do
  pid=${d#/proc/}
  target=$(readlink "$d/exe" 2>/dev/null || true)
  if [ "$target" = "$exe" ]; then
    kill -KILL "$pid" 2>/dev/null || true
  fi
done
cd %s || exit 1
nohup "$exe" >%s 2>&1 < /dev/null &
	) >/dev/null 2>&1 &`, shellQuote(exe), shellQuote(workDir), shellQuote(logPath))
	cmd := exec.Command("/bin/sh", "-c", script)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
