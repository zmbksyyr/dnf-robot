package webadmin

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
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

	"robot/internal/config"
)

type Server struct {
	cfg       *config.SysConfig
	robotAddr string
	webAddr   string
	token     string
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
		token:     fmt.Sprintf("%x", time.Now().UnixNano()),
	}
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/call", s.requireAuth(s.handleCall))
	mux.HandleFunc("/api/game-port", s.requireAuth(s.handleGamePort))
	mux.HandleFunc("/api/keypair-download", s.requireAuth(s.handleKeypairDownload))
	mux.HandleFunc("/api/log", s.requireAuth(s.handleLog))
	server := &http.Server{
		Addr:              s.webAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("[WebAdmin] listening on %s, robot=%s\n", s.webAddr, s.robotAddr)
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
	_ = indexTemplate.Execute(w, map[string]interface{}{
		"RobotAddr": s.robotAddr,
		"WebAddr":   s.webAddr,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeLogin(w, "")
		return
	}
	_ = r.ParseForm()
	password := r.Form.Get("password")
	if s.cfg.WebPassword == "" || subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.WebPassword)) == 1 {
		http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.writeLogin(w, "password error")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) writeLogin(w http.ResponseWriter, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTemplate.Execute(w, map[string]string{"Error": errText})
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
	if s.cfg.WebPassword == "" {
		return true
	}
	c, err := r.Cookie("tw_web_token")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1
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
	cmd := exec.Command("sh", "-c", fmt.Sprintf("ss -lntp 2>/dev/null | grep ':%d ' | head -n1", port))
	data, err := cmd.Output()
	if err != nil || len(data) == 0 {
		return "", false
	}
	re := regexp.MustCompile(`pid=([0-9]+)`)
	m := re.FindSubmatch(data)
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

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/root/robot.log"
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(s.cfg.ConfigDir, "log_robot")
		}
	}
	lines := tailFile(path, 220)
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "text": strings.Join(lines, "\n")})
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
	gameDir := filepath.Dir(s.cfg.DFGameR)
	files := []struct {
		name string
		path string
	}{
		{name: "privatekey.pem", path: filepath.Join(gameDir, "privatekey.pem")},
		{name: "publickey.pem", path: filepath.Join(gameDir, "publickey.pem")},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			_ = zw.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fw, err := zw.Create(file.name)
		if err != nil {
			_ = zw.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := fw.Write(data); err != nil {
			_ = zw.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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

func tailFile(path string, maxLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{err.Error()}
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
	}
	if err := scanner.Err(); err != nil {
		lines = append(lines, err.Error())
	}
	return lines
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>TW Robot</title><style>
:root{color-scheme:light;--bg:#eef2f6;--panel:#fff;--line:#d7dee8;--text:#17202a;--accent:#2563eb;--danger:#b91c1c}
*{box-sizing:border-box}body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",Arial,sans-serif;margin:0;background:var(--bg);color:var(--text)}
.box{width:340px;margin:14vh auto;background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:24px;box-shadow:0 18px 50px rgba(15,23,42,.12)}
h2{margin:0 0 16px;font-size:22px}input,button{width:100%;padding:10px 11px;margin-top:10px;border-radius:6px;font-size:14px}
input{border:1px solid #aeb8c7}button{cursor:pointer;border:1px solid var(--accent);background:var(--accent);color:#fff;font-weight:600}.err{color:var(--danger);margin-top:12px;font-size:13px}
</style></head><body><form class="box" method="post" action="/login">
<h2>TW Robot</h2><input name="password" type="password" placeholder="Web 密码" autofocus>
<button>登录</button>{{if .Error}}<div class="err">密码错误</div>{{end}}</form></body></html>`))

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>TW Robot Web</title><style>
:root{color-scheme:light;--bg:#eef2f6;--panel:#fff;--line:#d7dee8;--line2:#edf1f5;--text:#17202a;--muted:#64748b;--accent:#2563eb;--accent2:#1d4ed8;--danger:#b91c1c;--ok:#15803d;--warn:#a16207}
*{box-sizing:border-box}body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",Arial,sans-serif;margin:0;background:var(--bg);color:var(--text);font-size:14px}
header{height:48px;display:flex;gap:16px;align-items:center;padding:0 16px;background:#17202a;color:#fff}header b{font-size:16px}header span{opacity:.86}header a{color:#fff;text-decoration:none}.muted{color:var(--muted)}
.wrap{padding:12px 14px}.statusline{display:grid;grid-template-columns:repeat(6,minmax(130px,1fr));gap:8px;margin-bottom:10px}.metric{background:var(--panel);border:1px solid var(--line);border-radius:7px;padding:9px 10px}.metric .k{font-size:12px;color:var(--muted);margin-bottom:3px}.metric .v{font-size:18px;font-weight:700}.metric.good .v{color:var(--ok)}.metric.bad .v{color:var(--danger)}
.toolbar{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:10px}.toolbar .spacer{flex:1}.toolbar label{display:flex;gap:5px;align-items:center;color:var(--muted)}
button{height:32px;padding:0 11px;border:1px solid #b8c2d0;background:#fff;border-radius:6px;cursor:pointer;color:var(--text);font:inherit}button:hover{border-color:#8fa0b8;background:#f8fafc}button:disabled{opacity:.55;cursor:not-allowed}button.primary{background:var(--accent);border-color:var(--accent);color:#fff}button.primary:hover{background:var(--accent2)}button.danger{border-color:#efb5b5;color:var(--danger)}button.ghost{background:transparent}
input,textarea{font:inherit;border:1px solid #b8c2d0;border-radius:6px;padding:7px;background:#fff}input[type=number]{width:92px}.grid{display:grid;grid-template-rows:minmax(280px,1fr) 210px;gap:10px;height:calc(100vh - 150px)}
.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}.panelhead{height:36px;display:flex;align-items:center;gap:10px;padding:0 10px;background:#f8fafc;border-bottom:1px solid var(--line)}.panelhead h3{margin:0;font-size:14px}.panelhead span{color:var(--muted);font-size:12px}
.tablebox{height:calc(100% - 36px);overflow:auto}table{width:100%;border-collapse:collapse}th,td{padding:7px 8px;border-bottom:1px solid var(--line2);text-align:center;white-space:nowrap}th{background:#f8fafc;position:sticky;top:0;z-index:2;color:#475569;font-size:12px}tbody tr:nth-child(even){background:#fbfdff}tbody tr:hover{background:#eff6ff}tr.sel{background:#dbeafe!important}
pre{margin:0;padding:10px;height:174px;overflow:auto;white-space:pre-wrap;font-family:Consolas,"Cascadia Mono",monospace;font-size:12px;line-height:1.45;background:#0f172a;color:#dbeafe}.toast{position:fixed;right:14px;bottom:14px;max-width:520px;background:#17202a;color:#fff;border-radius:7px;padding:10px 12px;box-shadow:0 12px 32px rgba(15,23,42,.22);display:none}
dialog{border:1px solid var(--line);border-radius:8px;padding:0;min-width:460px;max-width:min(920px,96vw);box-shadow:0 18px 70px rgba(15,23,42,.22)}dialog::backdrop{background:rgba(15,23,42,.32)}.dlghead{display:flex;align-items:center;justify-content:space-between;padding:12px 14px;border-bottom:1px solid var(--line);background:#f8fafc}.dlghead h3{margin:0;font-size:15px}.dlgbody{padding:14px}.dlgfoot{display:flex;justify-content:flex-end;gap:8px;padding:12px 14px;border-top:1px solid var(--line);background:#f8fafc}.formgrid{display:grid;grid-template-columns:160px 1fr;gap:10px 12px;align-items:center}.formgrid label{color:#475569}.configbox{width:min(860px,88vw);height:min(560px,70vh);font-family:Consolas,"Cascadia Mono",monospace;font-size:12px;line-height:1.45}.chk{display:flex;gap:7px;align-items:center}.chk input{width:auto}.pill{display:inline-block;padding:2px 7px;border-radius:999px;background:#e2e8f0;color:#334155;font-size:12px}.pill.ok{background:#dcfce7;color:#166534}.pill.warn{background:#fef3c7;color:#92400e}.pill.bad{background:#fee2e2;color:#991b1b}
@media(max-width:900px){.statusline{grid-template-columns:repeat(2,1fr)}.grid{height:auto;grid-template-rows:420px 220px}.toolbar .spacer{display:none}}
</style></head><body>
<header><b>TW Robot Web</b><span>Web {{.WebAddr}}</span><span>TCP {{.RobotAddr}}</span><span id="gameHead">Game: -</span><a href="/logout">退出</a></header>
<div class="wrap">
<div class="statusline">
<div class="metric" id="mRobot" title="robot 进程 CPU、内存和 goroutine 数"><div class="k">系统占用</div><div class="v">检测中</div></div>
<div class="metric" id="mGame" title="检测 10011 游戏端口；端口开放后读取当前频道 cfg 的 max_user_num"><div class="k">游戏端口</div><div class="v">检测中</div></div>
<div class="metric" id="mKey" title="检查 game 目录 privatekey.pem / publickey.pem"><div class="k">Key 状态</div><div class="v">检测中</div></div>
<div class="metric"><div class="k">自动模式</div><div class="v" id="mAuto">-</div></div>
<div class="metric"><div class="k">运行/目标</div><div class="v" id="mOnline">-</div></div>
<div class="metric"><div class="k">摆摊</div><div class="v" id="mStore">-</div></div>
</div>
<div class="toolbar">
<button class="primary" data-key-action="1" onclick="refreshAll()">刷新</button><button data-key-action="1" onclick="openAutoDialog()">自动行为</button><button data-key-action="1" onclick="openOnlineDialog()">上线</button><button data-key-action="1" onclick="runAction('robotsMove')">移动</button><button data-key-action="1" onclick="runAction('robotsShout')">喊话</button><button data-key-action="1" onclick="runAction('robotsStoreAsync')">摆摊</button><button data-key-action="1" onclick="runAction('robotsLogoutAsync')">下线</button><button data-key-action="1" class="danger" onclick="openCleanupDialog()">清理</button><button id="keyButton" onclick="openKeyDialog()">Key</button><button data-key-action="1" onclick="openConfigDialog()">配置</button><button data-key-action="1" onclick="loadLog()">日志</button>
<span class="spacer"></span><label class="chk"><input id="autoRefresh" type="checkbox" checked onchange="toggleAutoRefresh()">自动刷新</label><label>未选时操作数量 <input id="actionCount" type="number" min="1" max="500" value="1"></label><span class="muted" id="selectedInfo">已选 0</span>
</div>
<div class="grid">
<div class="panel"><div class="panelhead"><h3>假人列表</h3><span id="listInfo">等待刷新</span></div><div class="tablebox"><table id="tbl"><thead><tr>
<th><input type="checkbox" id="checkAll" onchange="toggleAll(this.checked)"></th><th>UID</th><th>CID</th><th>角色名</th><th>账号</th><th>状态</th><th>类型</th><th>等级</th><th>职业</th><th>城镇</th><th>区域</th><th>X</th><th>Y</th><th>存活</th><th>重连</th><th>摆摊</th>
</tr></thead><tbody></tbody></table></div></div>
<div class="panel"><div class="panelhead"><h3>日志</h3><span id="logInfo">命令输出和 robot 日志</span></div><pre id="log"></pre></div>
</div></div>
<dialog id="modal"><div class="dlghead"><h3 id="modalTitle"></h3><button class="ghost" onclick="closeModal()">关闭</button></div><div class="dlgbody" id="modalBody"></div><div class="dlgfoot" id="modalFoot"></div></dialog>
<div class="toast" id="toast"></div>
<script>
const selected = new Set(); let currentRows = []; let busy = false; let keyBlocked = false; let autoRefreshTimer = null;
function byId(id){return document.getElementById(id)}
function text(v){return String(v ?? '')}
function escapeHTML(v){return text(v).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function toast(msg){const t=byId('toast');t.textContent=msg;t.style.display='block';clearTimeout(t._timer);t._timer=setTimeout(()=>t.style.display='none',3600)}
function actionName(cmd){return ({robotsOnlineAsync:'上线',robotsStoreAsync:'摆摊',robotsLogoutAsync:'下线',cleanupRobotsAsync:'清理',robotsMove:'移动',robotsShout:'喊话'}[cmd]||cmd)}
function applyKeyGate(){document.querySelectorAll('[data-key-action="1"]').forEach(b=>{b.disabled=busy||keyBlocked});const kb=byId('keyButton');if(kb)kb.disabled=busy}
function setBusy(v){busy=v;document.querySelectorAll('button').forEach(b=>{b.disabled=v&&!b.classList.contains('ghost')});applyKeyGate()}
async function api(command,payload={}){const r=await fetch('/api/call',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({command,payload})});const x=await r.json().catch(()=>({ok:false,error:'invalid json'}));if(!r.ok||!x.ok)throw new Error(x.error||('HTTP '+r.status));return x}
function resultOf(x){return x&&x.result&&x.result.result?x.result.result:x.result}
function writeLog(v){byId('log').textContent=typeof v==='string'?v:JSON.stringify(v,null,2)}
function uptime(sec){sec=Number(sec||0);const h=Math.floor(sec/3600),m=Math.floor((sec%3600)/60),s=sec%60;return h?(h+':'+String(m).padStart(2,'0')+':'+String(s).padStart(2,'0')):(m+':'+String(s).padStart(2,'0'))}
function selectedPayload(){const u=[...selected].map(Number);if(u.length)return {uids:u};return {count:Math.max(1,Number(byId('actionCount').value||1))}}
function updateSelectedInfo(){byId('selectedInfo').textContent='已选 '+selected.size;byId('checkAll').checked=currentRows.length>0 && currentRows.every(r=>selected.has(String(r.uid||'')))}
async function guarded(name,fn){if(busy)return;setBusy(true);try{const out=await fn();if(out!==undefined)writeLog(out);const queued=JSON.stringify(out||{}).includes('"state":"queued"');toast(name+(queued?' 已提交后台执行':' 完成'))}catch(e){writeLog({ok:false,error:e.message});toast(name+' 失败: '+e.message)}finally{setBusy(false)}}
async function refreshAll(){await guarded('刷新',async()=>{await Promise.all([refreshStatus(),refreshRobots(),checkGame(),refreshKeypair()]);return '刷新完成 '+new Date().toLocaleTimeString()})}
async function refreshQuiet(){if(busy||!byId('autoRefresh')?.checked)return;try{await Promise.all([refreshStatus(),refreshRobots(),checkGame(),refreshKeypair()])}catch(e){}}
async function refreshStatus(){const x=await api('autoStatus');let r=resultOf(x);if(r&&r.result)r=r.result;if(r){byId('mAuto').textContent=r.enabled?'开启':'关闭';byId('mOnline').textContent=(r.running||0)+' / '+(r.target_online||0);byId('mStore').textContent=String(r.store_running||0)}try{const sx=await api('systemStatus');let s=resultOf(sx);if(s&&s.result)s=s.result;const el=byId('mRobot');el.className='metric good';const cpu=Number(s.robot_cpu_percent||0).toFixed(1);el.querySelector('.v').textContent=cpu+'% '+(s.robot_memory_mb||0)+'MB';el.querySelector('.k').textContent='系统占用 / '+(s.robot_threads||0)+' goroutines'}catch(e){const el=byId('mRobot');el.className='metric bad';el.querySelector('.v').textContent='错误'}}
async function refreshRobots(){const x=await api('robotsStatus',{count:10000});const r=resultOf(x)||{};currentRows=r.robots||[];const tb=document.querySelector('#tbl tbody');tb.textContent='';for(const a of currentRows){const uid=String(a.uid||'');const tr=document.createElement('tr');if(selected.has(uid))tr.className='sel';const cb=document.createElement('input');cb.type='checkbox';cb.checked=selected.has(uid);appendCell(tr,cb);[uid,a.cid,a.name,a.account,a.state_name,a.robot_type,a.level,(text(a.job)+'/'+text(a.grow)),a.village,a.area,a.x,a.y,uptime(a.uptime_seconds),a.reconnects].forEach(v=>appendCell(tr,text(v)));const store=a.store_display_ack?'成功':((a.store_created||a.robot_type==2||a.robot_type==3)?'准备':'');const span=document.createElement('span');if(store){span.className='pill '+(a.store_display_ack?'ok':'warn');span.textContent=store}appendCell(tr,span);tr.onclick=e=>{if(e.target.tagName!=='INPUT')cb.checked=!cb.checked;cb.checked?selected.add(uid):selected.delete(uid);tr.className=selected.has(uid)?'sel':'';updateSelectedInfo()};tb.appendChild(tr)}byId('listInfo').textContent='共 '+currentRows.length+' 个，最后刷新 '+new Date().toLocaleTimeString();updateSelectedInfo()}
function appendCell(tr,v){const td=document.createElement('td');if(v instanceof Node)td.appendChild(v);else td.textContent=v;tr.appendChild(td)}
async function checkGame(){const x=await fetch('/api/game-port').then(r=>r.json());byId('gameHead').textContent='Game: '+(x.ok?'open':'closed')+' '+(x.addr||'');byId('mGame').className='metric '+(x.ok?'good':'bad');let txt=x.ok?'已开放':'未开放';if(x.ok&&x.max_user_num)txt+=' / '+x.max_user_num;byId('mGame').querySelector('.v').textContent=txt;byId('mGame').title=x.game_cfg_name?('配置 '+x.game_cfg_name+'.cfg，max_user_num='+x.max_user_num):'检测 10011 游戏端口；端口开放后读取当前频道 cfg 的 max_user_num'}
async function loadLog(){await guarded('读取日志',async()=>{const x=await fetch('/api/log').then(r=>r.json());return (x.path?x.path+'\n':'')+(x.text||'')})}
async function runAction(cmd){await guarded(actionName(cmd),async()=>{const x=await api(cmd,selectedPayload());setTimeout(()=>refreshAll(),800);return x})}
function toggleAll(checked){if(checked){currentRows.forEach(r=>selected.add(String(r.uid||'')))}else{selected.clear()}document.querySelectorAll('#tbl tbody tr').forEach(tr=>{const cb=tr.querySelector('input');if(cb)cb.checked=checked;tr.className=checked?'sel':''});updateSelectedInfo()}
function closeModal(){byId('modal').close()}
function showModal(title,body,foot){byId('modalTitle').textContent=title;byId('modalBody').innerHTML=body;byId('modalFoot').innerHTML=foot||'<button onclick="closeModal()">关闭</button>';byId('modal').showModal()}
function openOnlineDialog(){if(selected.size){runAction('robotsOnlineAsync');return}showModal('手动上线','<div class="formgrid"><label>上线数量</label><input id="onlineCount" type="number" min="1" max="500" value="10"></div>','<button onclick="closeModal()">取消</button><button class="primary" onclick="submitOnline()">上线</button>')}
async function submitOnline(){const c=Math.max(1,Number(byId('onlineCount').value||1));closeModal();await guarded('上线',async()=>{const x=await api('robotsOnlineAsync',{count:c});setTimeout(()=>refreshAll(),1000);return x})}
function openCleanupDialog(){const target=selected.size?('选中的 '+selected.size+' 个假人'):'全部 robot 假人';showModal('清理假人','<p>将删除 '+escapeHTML(target)+'。cleanupRobots 会按 registry、账号 UID、Dummylist 条件限制，避免误删普通角色。</p>','<button onclick="closeModal()">取消</button><button class="danger" onclick="submitCleanup()">确认清理</button>')}
async function submitCleanup(){const p=selected.size?{uids:[...selected].map(Number),force:true}:{force:true};closeModal();await guarded('清理',async()=>{const x=await api('cleanupRobotsAsync',p);selected.clear();setTimeout(()=>refreshAll(),1000);return x})}
async function openAutoDialog(){await guarded('读取自动配置',async()=>{const x=await api('robotConfigGet');const cfg=(resultOf(x)||{}).config||{};showModal('自动行为','<div class="formgrid"><label>自动开关</label><label class="chk"><input id="autoEnabled" type="checkbox" '+(String(cfg.auto_actions)==='true'?'checked':'')+'>启用</label><label>在线目标</label><input id="autoTarget" type="number" min="1" max="1000" value="'+escapeHTML(cfg.auto_target_online_count||600)+'"><label>每轮上线上限</label><input id="onlineBatch" type="number" min="1" max="120" value="'+escapeHTML(cfg.scheduler_online_batch_size||120)+'"><label>每秒启动数</label><input id="onlineRate" type="number" min="1" max="60" value="'+escapeHTML(cfg.scheduler_online_start_rate||20)+'"><label>填满目标秒数</label><input id="onlineFill" type="number" min="1" value="'+escapeHTML(cfg.scheduler_online_fill_timeout_sec||60)+'"><label>摆摊概率 %</label><input id="storeProb" type="number" min="0" max="100" value="'+escapeHTML(cfg.auto_store_probability_percent||20)+'"><label>摆摊最小秒</label><input id="storeMin" type="number" min="30" value="'+escapeHTML(cfg.auto_store_interval_min_sec||60)+'"><label>摆摊最大秒</label><input id="storeMax" type="number" min="30" value="'+escapeHTML(cfg.auto_store_interval_max_sec||180)+'"><label>喊话最小秒</label><input id="shoutMin" type="number" min="1" value="'+escapeHTML(cfg.auto_shout_interval_min_sec||45)+'"><label>喊话最大秒</label><input id="shoutMax" type="number" min="1" value="'+escapeHTML(cfg.auto_shout_interval_max_sec||120)+'"></div>','<button onclick="closeModal()">取消</button><button onclick="submitAuto(false)">保存并停止</button><button class="primary" onclick="submitAuto(true)">保存并启动</button>');return undefined})}
async function submitAuto(forceStart){const enabled=forceStart||byId('autoEnabled').checked;const updates={'auto.auto_target_online_count':String(byId('autoTarget').value||600),'scheduler.online_batch_size':String(byId('onlineBatch').value||120),'scheduler.online_start_rate':String(byId('onlineRate').value||20),'scheduler.online_fill_timeout_sec':String(byId('onlineFill').value||60),'auto.auto_store_probability_percent':String(byId('storeProb').value||20),'auto.auto_store_interval_min_sec':String(byId('storeMin').value||60),'auto.auto_store_interval_max_sec':String(byId('storeMax').value||180),'auto.auto_shout_interval_min_sec':String(byId('shoutMin').value||45),'auto.auto_shout_interval_max_sec':String(byId('shoutMax').value||120),'auto.auto_actions':String(enabled)};closeModal();await guarded('自动行为',async()=>{const a=await api('robotConfigUpdate',{updates});const b=await api(enabled?'autoStart':'autoStop');setTimeout(()=>refreshAll(),1000);return {config:a,auto:b}})}
async function refreshKeypair(){try{const x=await api('keypairStatus');const r=resultOf(x)||{};const el=byId('mKey');keyBlocked=!(r.key_state==='default'||r.key_state==='user'||r.game_valid);applyKeyGate();el.className='metric '+(keyBlocked?'bad':'good');el.querySelector('.k').textContent='Key 状态';el.querySelector('.v').textContent=r.key_state_label||r.key_state||'缺失'}catch(e){keyBlocked=true;applyKeyGate();const el=byId('mKey');el.className='metric bad';el.querySelector('.v').textContent='检测失败'}}
async function openKeyDialog(){await guarded('Key',async()=>{const x=await api('keypairStatus');const r=resultOf(x)||{};const rows=[['状态',r.key_state_label||r.key_state||'缺失'],['原因',r.key_reason||'-'],['game 私钥',r.game_private||'-'],['game 公钥',r.game_public||'-'],['指纹',r.fingerprint||'-']];let html='<div class="formgrid">';for(const row of rows){html+='<label>'+escapeHTML(row[0])+'</label><div>'+escapeHTML(row[1])+'</div>'}html+='</div>';html+=r.game_valid?'<p class="muted">当前 game 目录密钥对合法；下载的是当前已验证的密钥对。</p>':'<p class="muted">请在 game 目录配置合法 RSA 密钥对，或者点击释放默认 Key。</p>';const releaseDis=(r.can_release_default&&r.default_valid)?'':' disabled';const downloadDis=r.can_download_current?'':' disabled';showModal('RSA Key 管理',html,'<button onclick="closeModal()">关闭</button><button'+downloadDis+' onclick="downloadCurrentKeypair()">下载当前 Key</button><button class="primary"'+releaseDis+' onclick="releaseDefaultKeypair()">释放默认 Key</button>');return undefined})}
async function releaseDefaultKeypair(){closeModal();await guarded('释放默认 Key',async()=>{const x=await api('keypairReleaseDefault',{});await refreshKeypair();return x})}
function downloadCurrentKeypair(){window.location='/api/keypair-download'}
async function openConfigDialog(){await guarded('读取配置',async()=>{const x=await api('robotConfigGet');const body='<textarea id="cfgText" class="configbox"></textarea>';showModal('robot_config.ini',body,'<button onclick="closeModal()">取消</button><button class="primary" onclick="saveConfig()">保存</button>');byId('cfgText').value=(resultOf(x)||{}).text||'';return undefined})}
async function saveConfig(){const text=byId('cfgText').value;closeModal();await guarded('保存配置',async()=>{const x=await api('robotConfigUpdate',{text});setTimeout(()=>refreshAll(),1000);return x})}
function toggleAutoRefresh(){if(autoRefreshTimer){clearInterval(autoRefreshTimer);autoRefreshTimer=null}if(byId('autoRefresh')?.checked){autoRefreshTimer=setInterval(refreshQuiet,8000)}}
refreshAll();toggleAutoRefresh();
</script></body></html>`))
