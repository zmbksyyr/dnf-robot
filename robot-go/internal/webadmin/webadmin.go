package webadmin

import (
	"archive/zip"
	"bytes"
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
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"robot/internal/config"
	"robot/internal/service"
)

type Server struct {
	cfg       *config.SysConfig
	robotAddr string
	webAddr   string
	tokenMu   sync.RWMutex
	tokens    map[string]time.Time
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
	mux.HandleFunc("/api/game-hook", s.requireAuth(s.handleGameHook))
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
	writeJSON(w, map[string]interface{}{"ok": true, "raw": raw, "result": parseRobotResult(raw)})
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

type gameHookProcess struct {
	PID    int    `json:"pid"`
	Loaded bool   `json:"loaded"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) handleGameHook(w http.ResponseWriter, _ *http.Request) {
	const library = "libantisvrinline.so"
	processes, err := dfGameProcesses()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "library": library, "error": err.Error()})
		return
	}
	out := map[string]interface{}{
		"ok":        false,
		"library":   library,
		"total":     len(processes),
		"loaded":    0,
		"processes": processes,
		"state":     "no_process",
	}
	if len(processes) == 0 {
		writeJSON(w, out)
		return
	}
	loaded := 0
	for i := range processes {
		maps, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(processes[i].PID), "maps"))
		if err != nil {
			processes[i].Error = err.Error()
			continue
		}
		processes[i].Loaded = bytes.Contains(maps, []byte(library))
		if processes[i].Loaded {
			loaded++
		}
	}
	out["loaded"] = loaded
	out["processes"] = processes
	switch {
	case loaded == len(processes):
		out["ok"] = true
		out["state"] = "loaded"
	case loaded > 0:
		out["state"] = "partial"
	default:
		out["state"] = "missing"
	}
	writeJSON(w, out)
}

func dfGameProcesses() ([]gameHookProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := []gameHookProcess{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		for _, part := range parts {
			if strings.HasSuffix(part, "df_game_r") || part == "df_game_r" || strings.Contains(part, "/df_game_r") {
				out = append(out, gameHookProcess{PID: pid})
				break
			}
		}
	}
	return out, nil
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
	defaultPrivate, defaultPublic, err := service.DefaultKeypairPEM()
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
			if strings.Contains(buf.String(), "</tw>") {
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
