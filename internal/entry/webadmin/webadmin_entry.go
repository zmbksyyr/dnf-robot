package webadmin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"robot/internal/capability/keypair"
	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ---- webadmin.go ----
type Server struct {
	cfg           *config.SysConfig
	robotAddr     string
	webAddr       string
	tokenMu       lockhub.RWLocker
	tokens        map[string]time.Time
	partyCompatMu lockhub.Locker
}

type callRequest struct {
	Command string                 `json:"command"`
	Payload map[string]interface{} `json:"payload"`
}

func New(cfg *config.SysConfig, robotAddr, webAddr string) *Server {
	if robotAddr == "" {
		robotAddr = fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	}
	if webAddr == "" {
		webAddr = fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	}
	return &Server{
		cfg:       cfg,
		robotAddr: robotAddr,
		webAddr:   webAddr,
		tokens:    make(map[string]time.Time),
	}
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/call", s.requireAuth(s.handleCall))
	mux.HandleFunc("/api/game-port", s.requireAuth(s.handleGamePort))
	mux.HandleFunc("/api/game-endpoint", s.requireAuth(s.handleGameEndpoint))
	mux.HandleFunc("/api/restart-robot", s.requireAuth(s.handleRestartRobot))
	mux.HandleFunc("/api/max-user", s.requireAuth(s.handleMaxUser))
	mux.HandleFunc("/api/server-script", s.requireAuth(s.handleServerScript))
	mux.HandleFunc("/api/monitor-service", s.requireAuth(s.handleMonitorService))
	mux.HandleFunc("/api/relay-service", s.requireAuth(s.handleRelayService))
	mux.HandleFunc("/api/party-compat", s.requireAuth(s.handlePartyCompat))
	mux.HandleFunc("/api/keypair-download", s.requireAuth(s.handleKeypairDownload))
	server := &http.Server{
		Addr:              s.webAddr,
		Handler:           s.withDiagnostics(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("[WebAdmin] listening on %s, robot=%s pid=%d sessions=%d\n", s.webAddr, s.robotAddr, os.Getpid(), s.sessionCount())
	return server.ListenAndServe()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.authed(r) {
		s.writeLogin(w, "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cleanIndexTemplate.Execute(w, map[string]interface{}{
		"RobotAddr": s.robotAddr,
		"WebAddr":   s.webAddr,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeLogin(w, "")
		return
	}
	_ = r.ParseForm()
	password := r.Form.Get("password")
	if strings.TrimSpace(s.cfg.WebPassword) == "" {
		s.writeLogin(w, "web password is not configured")
		return
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.WebPassword)) == 1 {
		token := randomToken()
		s.tokenMu.Lock()
		s.cleanupExpiredTokensLocked(time.Now())
		s.tokens[token] = time.Now().Add(12 * time.Hour)
		active := len(s.tokens)
		s.tokenMu.Unlock()
		fmt.Printf("[WebAdmin] session created pid=%d active=%d remote=%s\n", os.Getpid(), active, r.RemoteAddr)
		http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.writeLogin(w, "password error")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("tw_web_token"); err == nil {
		s.tokenMu.Lock()
		delete(s.tokens, c.Value)
		s.tokenMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) writeLogin(w http.ResponseWriter, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cleanLoginTemplate.Execute(w, map[string]string{"Error": errText}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authed(r *http.Request) bool {
	if strings.TrimSpace(s.cfg.WebPassword) == "" {
		return false
	}
	c, err := r.Cookie("tw_web_token")
	if err != nil {
		return false
	}
	if c.Value == "" {
		fmt.Printf("[WebAdmin] auth rejected pid=%d reason=empty_token path=%s remote=%s\n", os.Getpid(), r.URL.Path, r.RemoteAddr)
		return false
	}
	now := time.Now()
	s.tokenMu.Lock()
	expires, ok := s.tokens[c.Value]
	if ok && now.After(expires) {
		delete(s.tokens, c.Value)
		ok = false
	}
	if ok {
		s.tokens[c.Value] = now.Add(12 * time.Hour)
	}
	s.cleanupExpiredTokensLocked(now)
	active := len(s.tokens)
	s.tokenMu.Unlock()
	if !ok {
		fmt.Printf("[WebAdmin] auth rejected pid=%d reason=unknown_or_expired_token active=%d path=%s remote=%s\n", os.Getpid(), active, r.URL.Path, r.RemoteAddr)
	}
	return ok
}

func (s *Server) sessionCount() int {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return len(s.tokens)
}

func (s *Server) cleanupExpiredTokensLocked(now time.Time) {
	for token, expires := range s.tokens {
		if now.After(expires) {
			delete(s.tokens, token)
		}
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (s *Server) withDiagnostics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			duration := time.Since(start)
			if v := recover(); v != nil {
				fmt.Printf("[WebAdmin] panic pid=%d method=%s path=%s remote=%s duration=%s err=%v\n%s\n", os.Getpid(), r.Method, r.URL.Path, r.RemoteAddr, duration.Round(time.Millisecond), v, debug.Stack())
				http.Error(rec, "internal server error", http.StatusInternalServerError)
				return
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 500 || duration > 3*time.Second {
				fmt.Printf("[WebAdmin] request pid=%d method=%s path=%s status=%d bytes=%d duration=%s remote=%s\n", os.Getpid(), r.Method, r.URL.Path, status, rec.bytes, duration.Round(time.Millisecond), r.RemoteAddr)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req callRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "empty command"})
		return
	}
	raw, err := callRobot(s.robotAddr, req.Command, req.Payload, robotCallTimeout(req.Command), s.cfg.MaxResponseBytes)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	out := map[string]interface{}{"ok": true, "result": parseRobotResult(raw)}
	if r.URL.Query().Get("raw") == "1" {
		out["raw"] = raw
	}
	writeJSON(w, out)
}

func robotCallTimeout(command string) time.Duration {
	switch strings.TrimSpace(command) {
	case "robotsStore":
		return 90 * time.Second
	default:
		return 30 * time.Second
	}
}

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

func (s *Server) handleKeypairDownload(w http.ResponseWriter, _ *http.Request) {
	raw, err := callRobot(s.robotAddr, "keypairStatus", nil, robotCallTimeout("keypairStatus"), s.cfg.MaxResponseBytes)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	status, ok := parseRobotResult(raw).(map[string]interface{})
	if !ok {
		http.Error(w, "invalid keypair status", 502)
		return
	}
	result, ok := status["result"].(map[string]interface{})
	if !ok {
		http.Error(w, "invalid keypair result", 502)
		return
	}
	if valid, _ := result["game_valid"].(bool); !valid {
		http.Error(w, "game keypair is not valid", http.StatusConflict)
		return
	}
	defaultPrivate, defaultPublic, err := keypair.DefaultKeypairPEM()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := addZipFile(zw, "privatekey.pem", defaultPrivate); err != nil {
		_ = zw.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := addZipFile(zw, "publickey.pem", defaultPublic); err != nil {
		_ = zw.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := zw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tw_game_keypair.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

func addZipFile(zw *zip.Writer, name string, data []byte) error {
	fw, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write(data)
	return err
}

func randomToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("webadmin random token: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

func callRobot(addr, command string, payload map[string]interface{}, timeout time.Duration, maxResponseBytes int) (string, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if maxResponseBytes <= 0 {
		maxResponseBytes = 4 * 1024 * 1024
	}
	body, _ := json.Marshal(payload)
	packet := fmt.Sprintf("<tw><c>%s</c><json>%s</json></tw>", command, body)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(packet)); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	tmp := make([]byte, 64*1024)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if bytes.Contains(buf.Bytes(), []byte("</tw>")) {
				return buf.String(), nil
			}
			if buf.Len() > maxResponseBytes {
				return "", fmt.Errorf("robot response too large")
			}
		}
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return buf.String(), nil
			}
			return "", err
		}
	}
}

func parseRobotResult(raw string) interface{} {
	startTag := "<result>"
	endTag := "</result>"
	start := strings.Index(raw, startTag)
	if start < 0 {
		return nil
	}
	start += len(startTag)
	end := strings.Index(raw[start:], endTag)
	if end < 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal([]byte(raw[start:start+end]), &out); err != nil {
		return nil
	}
	return out
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// ---- supervisor.go ----
func StartSupervisor(cfg *config.SysConfig) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			cmd := newCommand(cfg)
			if cmd == nil {
				return
			}
			if err := cmd.Start(); err != nil {
				foundationlog.Robotf("web admin start failed: %v\n", err)
				select {
				case <-stop:
					return
				case <-time.After(3 * time.Second):
					continue
				}
			}
			pid := 0
			if cmd.Process != nil {
				pid = cmd.Process.Pid
			}
			foundationlog.Robotf("web admin listening on %s pid=%d\n", fmt.Sprintf("0.0.0.0:%d", cfg.WebPort), pid)
			waitCh := make(chan error, 1)
			go func() { waitCh <- cmd.Wait() }()
			select {
			case <-stop:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
					select {
					case <-waitCh:
					case <-time.After(500 * time.Millisecond):
						_ = cmd.Process.Kill()
						<-waitCh
					}
				}
				return
			case err := <-waitCh:
				foundationlog.Robotf("web admin exited pid=%d err=%v; restarting\n", pid, err)
				select {
				case <-stop:
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func newCommand(cfg *config.SysConfig) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		foundationlog.Robotf("web admin executable lookup failed: %v\n", err)
		return nil
	}
	robotAddr := fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	webAddr := fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	cmd := exec.Command(exe, "--web-admin", "--robot-addr", robotAddr, "--web-addr", webAddr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
