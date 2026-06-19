package webadmin

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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
	"sync"
	"time"

	"robot/internal/config"
)

type Server struct {
	cfg       *config.SysConfig
	robotAddr string
	webAddr   string
	tokenMu   sync.RWMutex
	token     string
}

type callRequest struct {
	Command string                 `json:"command"`
	Payload map[string]interface{} `json:"payload"`
}

var cleanLoginTemplate = template.Must(template.New("clean_login").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Robot Login</title><style>
body{font-family:system-ui,-apple-system,Segoe UI,Arial,sans-serif;background:#eef2f6;color:#17202a;margin:0;display:grid;place-items:center;min-height:100vh}
form{width:320px;background:#fff;border:1px solid #d7dee8;border-radius:8px;padding:22px;box-shadow:0 16px 45px rgba(15,23,42,.12)}
h1{font-size:18px;margin:0 0 16px}label{display:block;font-size:13px;color:#64748b;margin-bottom:6px}input{width:100%;box-sizing:border-box;height:36px;border:1px solid #b8c2d0;border-radius:6px;padding:0 10px;font:inherit}button{width:100%;height:36px;margin-top:14px;border:0;border-radius:6px;background:#2563eb;color:white;font:inherit;cursor:pointer}.err{margin-top:10px;color:#b91c1c;font-size:13px}
</style></head><body><form method="post" action="/login"><h1>Robot Web</h1><label>Password</label><input name="password" type="password" autofocus><button>Login</button>{{if .Error}}<div class="err">{{.Error}}</div>{{end}}</form></body></html>`))

var cleanIndexTemplate = template.Must(template.New("clean_index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>TW Robot Web</title><style>
:root{--bg:#eef2f6;--panel:#fff;--line:#d7dee8;--soft:#f8fafc;--text:#17202a;--muted:#64748b;--blue:#2563eb;--green:#15803d;--red:#b91c1c;--amber:#a16207}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px system-ui,-apple-system,Segoe UI,Arial,sans-serif}header{height:48px;display:flex;align-items:center;gap:16px;padding:0 16px;background:#17202a;color:#fff}header b{font-size:16px}header span{opacity:.85}header a{margin-left:auto;color:#fff;text-decoration:none}.wrap{padding:12px 14px}.cards{display:grid;grid-template-columns:repeat(7,minmax(120px,1fr));gap:8px;margin-bottom:10px}.card,.scheduler,.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px}.card{padding:9px 10px}.k{font-size:12px;color:var(--muted)}.v{font-size:18px;font-weight:700;margin-top:3px}.good .v{color:var(--green)}.bad .v{color:var(--red)}.toolbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:10px}.toolbar .spacer{flex:1}button{height:32px;border:1px solid #b8c2d0;border-radius:6px;background:#fff;color:var(--text);padding:0 11px;font:inherit;cursor:pointer}button:hover{background:#f8fafc}button:disabled{opacity:.55;cursor:not-allowed}.primary{background:var(--blue);border-color:var(--blue);color:#fff}.primary:hover{background:var(--blue);border-color:var(--blue);color:#fff}.danger{border-color:#efb5b5;color:var(--red)}input,textarea{font:inherit;border:1px solid #b8c2d0;border-radius:6px;padding:7px;background:#fff}input[type=number]{width:92px}.scheduler{padding:10px;margin-bottom:10px}.schedgrid{display:grid;grid-template-columns:repeat(6,minmax(120px,1fr));gap:8px}.scheditem{border:1px solid #edf1f5;border-radius:6px;padding:7px 8px;background:#fbfdff}.schedreason{margin-top:8px;color:var(--muted);font-size:12px}.grid{display:grid;grid-template-rows:minmax(280px,1fr) 210px;gap:10px;height:calc(100vh - 150px)}.panel{overflow:hidden}.panelhead{height:36px;display:flex;align-items:center;gap:10px;padding:0 10px;background:var(--soft);border-bottom:1px solid var(--line)}.panelhead h3{margin:0;font-size:14px}.panelhead span{color:var(--muted);font-size:12px}.tablebox{height:calc(100% - 36px);overflow:auto}table{width:100%;border-collapse:collapse}th,td{padding:7px 8px;border-bottom:1px solid #edf1f5;text-align:center;white-space:nowrap}th{background:var(--soft);position:sticky;top:0;z-index:2;color:#475569;font-size:12px}tbody tr:nth-child(even){background:#fbfdff}tbody tr:hover{background:#eff6ff}tr.sel,.bulk-selected tr{background:#dbeafe!important}.pill{display:inline-block;padding:2px 7px;border-radius:999px;background:#e2e8f0;color:#334155;font-size:12px}.pill.ok{background:#dcfce7;color:#166534}.pill.warn{background:#fef3c7;color:#92400e}pre{margin:0;height:174px;overflow:auto;white-space:pre-wrap;background:#0f172a;color:#dbeafe;padding:10px;font:12px/1.45 Consolas,monospace}.toast{position:fixed;right:14px;bottom:14px;max-width:520px;background:#17202a;color:#fff;border-radius:7px;padding:10px 12px;box-shadow:0 12px 32px rgba(15,23,42,.22);display:none}dialog{border:1px solid var(--line);border-radius:8px;padding:0;min-width:460px;max-width:min(920px,96vw);box-shadow:0 18px 70px rgba(15,23,42,.22)}dialog::backdrop{background:rgba(15,23,42,.32)}.dlghead,.dlgfoot{display:flex;align-items:center;padding:12px 14px;background:var(--soft);border-bottom:1px solid var(--line)}.dlghead h3{margin:0;font-size:15px}.dlghead button{margin-left:auto}.dlgbody{padding:14px}.dlgfoot{justify-content:flex-end;gap:8px;border-top:1px solid var(--line);border-bottom:0}.formgrid{display:grid;grid-template-columns:160px 1fr;gap:10px 12px;align-items:center}.muted{color:var(--muted)}.configbox{width:min(860px,88vw);height:min(560px,70vh);font:12px/1.45 Consolas,monospace}@media(max-width:1100px){.cards{grid-template-columns:repeat(4,1fr)}}@media(max-width:900px){.cards,.schedgrid{grid-template-columns:repeat(2,1fr)}.grid{height:auto;grid-template-rows:420px 220px}}
</style></head><body>
<header><b>TW Robot Web</b><span>Web {{.WebAddr}}</span><span>TCP {{.RobotAddr}}</span><span id="gameHead">Game: -</span><a href="/logout">Logout</a></header>
<div class="wrap">
<div class="cards"><div class="card" id="mRobot"><div class="k">Robot</div><div class="v">-</div></div><div class="card" id="mGame"><div class="k">Game port</div><div class="v">-</div></div><div class="card" id="mDB"><div class="k">Database</div><div class="v">-</div></div><div class="card" id="mKey"><div class="k">Key</div><div class="v">-</div></div><div class="card"><div class="k">Auto</div><div class="v" id="mAuto">-</div></div><div class="card"><div class="k">Online / Target</div><div class="v" id="mOnline">-</div></div><div class="card"><div class="k">Store</div><div class="v" id="mStore">-</div></div></div>
<div class="toolbar"><button class="primary" onclick="refreshAll()">Refresh</button><button data-key-action="1" onclick="openAutoDialog()">Auto</button><button data-key-action="1" onclick="openOnlineDialog()">Online</button><button data-key-action="1" onclick="runAction('robotsMove')">Move</button><button data-key-action="1" onclick="runAction('robotsShout')">Shout</button><button data-key-action="1" onclick="runAction('robotsStoreAsync')">Store</button><button data-key-action="1" onclick="runAction('robotsLogoutAsync')">Logout</button><button data-key-action="1" class="danger" onclick="openCleanupDialog()">Cleanup</button><button id="keyButton" onclick="openKeyDialog()">Key</button><span class="spacer"></span><label><input id="autoRefresh" type="checkbox" checked onchange="toggleAutoRefresh()"> Auto refresh</label><label>Count <input id="actionCount" type="number" min="1" max="1000" value="1"></label><span class="muted" id="selectedInfo">Selected 0</span></div>
<div class="scheduler"><div class="schedgrid"><div class="scheditem"><div class="k">Policy mode</div><div class="v" id="sMode">-</div></div><div class="scheditem"><div class="k">Attach pace</div><div class="v" id="sOnline">-</div></div><div class="scheditem"><div class="k">Store policy</div><div class="v" id="sStore">-</div></div><div class="scheditem"><div class="k">Scale window</div><div class="v" id="sScale">-</div></div><div class="scheditem"><div class="k">Pressure guard</div><div class="v" id="sPressure">-</div></div><div class="scheditem"><div class="k">Release guard</div><div class="v" id="sRelease">-</div></div></div><div class="schedreason" id="sReason">-</div></div>
<div class="grid"><div class="panel"><div class="panelhead"><h3>Robots</h3><span id="listInfo">-</span></div><div class="tablebox"><table id="tbl"><thead><tr><th><input type="checkbox" id="checkAll" onchange="toggleAll(this.checked)"></th><th>UID</th><th>CID</th><th>Name</th><th>Actor</th><th>State</th><th>Level</th><th>Job/Grow</th><th>Village</th><th>Area</th><th>X</th><th>Y</th><th>Uptime</th><th>Store</th></tr></thead><tbody></tbody></table></div></div><div class="panel"><div class="panelhead"><h3>Log</h3><span id="logInfo">/root/config/log_robot</span></div><pre id="log"></pre></div></div></div>
<dialog id="modal"><div class="dlghead"><h3 id="modalTitle"></h3><button onclick="closeModal()">Close</button></div><div class="dlgbody" id="modalBody"></div><div class="dlgfoot" id="modalFoot"></div></dialog><div class="toast" id="toast"></div>
<script>
const selected=new Set();let currentRows=[],busy=false,keyBlocked=false,dbBlocked=false,gameBlocked=false,autoRefreshTimer=null;
function byId(id){return document.getElementById(id)}function text(v){return String(v??'')}function escapeHTML(v){return text(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function toast(msg){const t=byId('toast');t.textContent=msg;t.style.display='block';clearTimeout(t._timer);t._timer=setTimeout(()=>t.style.display='none',3600)}
function actionName(cmd){return ({robotsOnlineAsync:'Online',robotsStoreAsync:'Store',robotsLogoutAsync:'Logout',cleanupRobotsAsync:'Cleanup',robotsMove:'Move',robotsShout:'Shout'}[cmd]||cmd)}
function setBusy(v){busy=v;document.querySelectorAll('button').forEach(b=>b.disabled=v);applyKeyGate()}function applyKeyGate(){const blocked=keyBlocked||dbBlocked||gameBlocked;document.querySelectorAll('[data-key-action="1"]').forEach(b=>b.disabled=busy||blocked);const kb=byId('keyButton');if(kb)kb.disabled=busy}
async function api(command,payload={}){const r=await fetch('/api/call',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({command,payload})});const x=await r.json().catch(()=>({ok:false,error:'invalid json'}));if(!r.ok||!x.ok)throw new Error(x.error||('HTTP '+r.status));return x}
function resultOf(x){return x&&x.result&&x.result.result?x.result.result:x.result}function writeLog(v){byId('log').textContent=typeof v==='string'?v:JSON.stringify(v,null,2)}
async function guarded(name,fn){if(busy)return;setBusy(true);try{const out=await fn();if(out!==undefined)writeLog(out);toast(name+' ok')}catch(e){writeLog({ok:false,error:e.message});toast(name+' failed: '+e.message)}finally{setBusy(false)}}
async function refreshAll(){await guarded('Refresh',async()=>{await Promise.all([refreshStatus(),refreshScheduler(),refreshRobots(),checkGame(),refreshDatabase(),refreshKeypair()]);return 'refreshed '+new Date().toLocaleTimeString()})}
async function refreshQuiet(){if(busy||!byId('autoRefresh')?.checked)return;try{await Promise.all([refreshStatus(),refreshScheduler(),refreshRobots(),checkGame(),refreshDatabase(),refreshKeypair()])}catch(e){}}
async function refreshStatus(){const x=await api('autoStatus');const r=resultOf(x)||{};byId('mAuto').textContent=r.enabled?'On':'Off';byId('mOnline').textContent=(r.running||0)+' / '+(r.target_online||0);byId('mStore').textContent=String(r.store_running||0);const sx=await api('systemStatus');const s=resultOf(sx)||{};const el=byId('mRobot');el.className='card good';el.querySelector('.v').textContent=Number(s.robot_cpu_percent||0).toFixed(1)+'% '+(s.robot_memory_mb||0)+'MB';el.querySelector('.k').textContent='Robot / '+(s.robot_threads||0)+' goroutines'}
async function refreshScheduler(){try{const x=await api('schedulerStatus');const s=resultOf(x)||{};byId('sMode').textContent=s.mode||'-';byId('sOnline').textContent=(s.online_start_rate||0)+'/s batch '+(s.online_batch_size||0);byId('sStore').textContent=(s.store_running||0)+'/'+(s.store_concurrent||0)+' p'+(s.store_probability_percent||0)+'%';byId('sScale').textContent='up '+(s.scale_up_batch||0)+' down '+(s.scale_down_batch||0);byId('sPressure').textContent='login '+(s.connecting||0)+' cpu '+Number(s.cpu_percent||0).toFixed(1)+'%';byId('sRelease').textContent='br '+(s.breaker_release_batch||0)+' port '+(s.port_down_release_batch||0);byId('sReason').textContent=(s.reason||'-')+' | running '+(s.running||0)+'/'+(s.target_online||0)+' idle '+(s.idle||0)+' port '+(s.game_port_ready?'ready':'down')+' breaker '+(s.breaker_active?'on':'off')+' | actor i/a/o/r/b/rel '+(s.actor_idle||0)+'/'+(s.actor_assigned||0)+'/'+(s.actor_online||0)+'/'+(s.actor_running||0)+'/'+(s.actor_busy||0)+'/'+(s.actor_releasing||0)}catch(e){byId('sMode').textContent='error';byId('sReason').textContent=e.message}}
function uptime(sec){sec=Number(sec||0);const h=Math.floor(sec/3600),m=Math.floor((sec%3600)/60),s=sec%60;return h?h+':'+String(m).padStart(2,'0')+':'+String(s).padStart(2,'0'):m+':'+String(s).padStart(2,'0')}
async function refreshRobots(){const x=await api('robotsStatus',{count:10000});const r=resultOf(x)||{};currentRows=r.robots||[];const tb=document.querySelector('#tbl tbody');tb.textContent='';tb.classList.remove('bulk-selected');for(const a of currentRows){const uid=String(a.uid||'');const tr=document.createElement('tr');if(selected.has(uid))tr.className='sel';const cb=document.createElement('input');cb.type='checkbox';cb.checked=selected.has(uid);appendCell(tr,cb);appendCell(tr,uid);appendCell(tr,text(a.cid));appendCell(tr,text(a.name));const actor=document.createElement('span');actor.className='pill '+(a.actor_attached?'ok':'');actor.textContent=a.actor_attached?('slot '+(a.actor_slot||'-')+' '+(a.actor_state||'')):'free';actor.title=a.actor_busy?('busy '+(a.actor_busy_kind||'')):'';appendCell(tr,actor);const state=document.createElement('span');state.className='pill '+(a.health_state==='ok'?'ok':(a.health_state==='suspect'?'warn':'bad'));state.textContent=(a.runtime_state||a.state_name||'-')+' -> '+(a.desired_state||'-');state.title='db '+(a.db_state||'-')+' health '+(a.health_state||'-')+(a.operation?' op '+a.operation:'');appendCell(tr,state);[a.level,text(a.job)+'/'+text(a.grow),a.village,a.area,a.x,a.y,uptime(a.uptime_seconds)].forEach(v=>appendCell(tr,text(v)));const store=a.store_display_ack?'active':((a.store_created||a.robot_type==2||a.robot_type==3)?'pending':'');const span=document.createElement('span');if(store){span.className='pill '+(store==='active'?'ok':'warn');span.textContent=store}appendCell(tr,span);tr.onclick=e=>{tb.classList.remove('bulk-selected');if(e.target.tagName!=='INPUT')cb.checked=!cb.checked;cb.checked?selected.add(uid):selected.delete(uid);tr.className=selected.has(uid)?'sel':'';updateSelectedInfo()};tb.appendChild(tr)}byId('listInfo').textContent=currentRows.length+' robots, '+new Date().toLocaleTimeString();updateSelectedInfo()}
function appendCell(tr,v){const td=document.createElement('td');if(v instanceof Node)td.appendChild(v);else td.textContent=v;tr.appendChild(td)}function selectedPayload(){const u=[...selected].map(Number);return u.length?{uids:u}:{count:Math.max(1,Number(byId('actionCount').value||1))}}function updateSelectedInfo(){byId('selectedInfo').textContent='Selected '+selected.size;byId('checkAll').checked=currentRows.length>0&&currentRows.every(r=>selected.has(String(r.uid||'')))}
async function checkGame(){const x=await fetch('/api/game-port').then(r=>r.json());gameBlocked=!x.ok;byId('gameHead').textContent='Game: '+(x.ok?'open':'closed')+' '+(x.addr||'');const el=byId('mGame');el.className='card '+(x.ok?'good':'bad');el.querySelector('.v').textContent=x.ok?'Open '+(x.max_user_num||''):'Closed';el.title=x.error||x.addr||'';applyKeyGate()}
async function refreshDatabase(){try{const x=await api('databaseStatus');const r=resultOf(x)||{};dbBlocked=!r.ok;const el=byId('mDB');el.className='card '+(dbBlocked?'bad':'good');el.querySelector('.v').textContent=r.ok?'OK '+(r.latency_ms||0)+'ms':'Error';el.title=r.ok?(r.target||''):(r.error||'database unavailable');applyKeyGate()}catch(e){dbBlocked=true;applyKeyGate();const el=byId('mDB');el.className='card bad';el.querySelector('.v').textContent='Error';el.title=e.message}}
async function refreshKeypair(){try{const x=await api('keypairStatus');const r=resultOf(x)||{};keyBlocked=!(r.key_state==='default'||r.key_state==='user'||r.game_valid);const el=byId('mKey');el.className='card '+(keyBlocked?'bad':'good');el.querySelector('.v').textContent=r.key_state_label||r.key_state||'-';applyKeyGate()}catch(e){keyBlocked=true;applyKeyGate();const el=byId('mKey');el.className='card bad';el.querySelector('.v').textContent='Error'}}
async function loadLog(){await guarded('Log',async()=>{const x=await fetch('/api/log').then(r=>r.json());return (x.path?x.path+'\n':'')+(x.text||'')})}async function runAction(cmd){await guarded(actionName(cmd),async()=>{const x=await api(cmd,selectedPayload());setTimeout(refreshAll,1000);return x})}
function toggleAll(checked){const tb=document.querySelector('#tbl tbody');if(checked){currentRows.forEach(r=>selected.add(String(r.uid||'')));tb.classList.add('bulk-selected')}else{selected.clear();tb.classList.remove('bulk-selected');document.querySelectorAll('#tbl tbody tr.sel').forEach(tr=>tr.className='');document.querySelectorAll('#tbl tbody input:checked').forEach(cb=>cb.checked=false)}updateSelectedInfo()}
function closeModal(){byId('modal').close()}function showModal(title,body,foot){byId('modalTitle').textContent=title;byId('modalBody').innerHTML=body;byId('modalFoot').innerHTML=foot||'<button onclick="closeModal()">Close</button>';byId('modal').showModal()}
function openOnlineDialog(){if(selected.size){runAction('robotsOnlineAsync');return}showModal('Online','<div class="formgrid"><label>Count</label><input id="onlineCount" type="number" min="1" max="1000" value="10"></div>','<button onclick="closeModal()">Cancel</button><button class="primary" onclick="submitOnline()">Online</button>')}async function submitOnline(){const c=Math.max(1,Number(byId('onlineCount').value||1));closeModal();await guarded('Online',async()=>api('robotsOnlineAsync',{count:c}))}
function openCleanupDialog(){const target=selected.size?selected.size+' selected robots':'all robot registry candidates';showModal('Cleanup','<p>Cleanup deletes robot registry/database rows for '+escapeHTML(target)+'. Normal shrink/logout should not use cleanup.</p>','<button onclick="closeModal()">Cancel</button><button class="danger" onclick="submitCleanup()">Cleanup</button>')}async function submitCleanup(){const p=selected.size?{uids:[...selected].map(Number),force:true}:{force:true};closeModal();await guarded('Cleanup',async()=>api('cleanupRobotsAsync',p))}
async function openAutoDialog(){await guarded('Read auto config',async()=>{const x=await api('robotConfigGet');const cfg=(resultOf(x)||{}).config||{};showModal('Auto','<div class="formgrid"><label>Enabled</label><label><input id="autoEnabled" type="checkbox" '+(String(cfg.auto_actions)==='true'?'checked':'')+'> On</label><label>Target online</label><input id="autoTarget" type="number" min="1" max="1000" value="'+escapeHTML(cfg.auto_target_online_count||20)+'"><label>Debug fixed spawn</label><label><input id="spawnFixed" type="checkbox" '+(String(cfg.spawn_fixed)==='true'?'checked':'')+'> fixed town / birth point</label><label>Town</label><input id="spawnVillage" type="number" min="1" max="3" value="'+escapeHTML(cfg.spawn_village||3)+'"><label>Birth area</label><input id="spawnArea" type="number" min="0" max="999" value="'+escapeHTML(cfg.spawn_area||0)+'"><div class="muted" style="grid-column:1/-1">Strategy derives login pace, actor scaling, store probability, breaker and release behavior automatically.</div></div>','<button onclick="closeModal()">Cancel</button><button onclick="submitAuto(false)">Save and pause</button><button class="primary" onclick="submitAuto(true)">Save and start</button>')})}
async function submitAuto(forceStart){const enabled=forceStart||byId('autoEnabled').checked;const updates={'auto.auto_target_online_count':String(byId('autoTarget').value||20),'auto.auto_actions':String(enabled),'spawn.spawn_fixed':String(byId('spawnFixed').checked),'spawn.spawn_village':String(byId('spawnVillage').value||3),'spawn.spawn_area':String(byId('spawnArea').value||0)};closeModal();await guarded('Auto',async()=>{const config=await api('robotConfigUpdate',{updates});const auto=await api(enabled?'autoStart':'autoStop');setTimeout(refreshAll,1000);return {config,auto}})}
async function openKeyDialog(){await guarded('Key',async()=>{const x=await api('keypairStatus');showModal('RSA Key','<pre>'+escapeHTML(JSON.stringify(resultOf(x)||{},null,2))+'</pre>','<button onclick="closeModal()">Close</button><button onclick="downloadCurrentKeypair()">Download</button><button class="primary" onclick="releaseDefaultKeypair()">Release default</button>')})}async function releaseDefaultKeypair(){closeModal();await guarded('Release key',async()=>api('keypairReleaseDefault',{}))}function downloadCurrentKeypair(){window.location='/api/keypair-download'}
async function openConfigDialog(){await guarded('Config',async()=>{const x=await api('robotConfigGet');showModal('robot_config.ini','<textarea id="cfgText" class="configbox"></textarea>','<button onclick="closeModal()">Cancel</button><button class="primary" onclick="saveConfig()">Save</button>');byId('cfgText').value=(resultOf(x)||{}).text||''})}async function saveConfig(){const text=byId('cfgText').value;closeModal();await guarded('Save config',async()=>api('robotConfigUpdate',{text}))}
function toggleAutoRefresh(){if(autoRefreshTimer){clearInterval(autoRefreshTimer);autoRefreshTimer=null}if(byId('autoRefresh')?.checked)autoRefreshTimer=setInterval(refreshQuiet,8000)}
refreshAll();toggleAutoRefresh();
</script></body></html>`))

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
		token:     randomToken(),
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
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
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
		s.token = token
		s.tokenMu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
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
	s.tokenMu.RLock()
	token := s.token
	s.tokenMu.RUnlock()
	return token != "" && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
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

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolveLogPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
	w.Header().Set("Content-Disposition", `attachment; filename="tw_game_public_key.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

func randomToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("webadmin random token: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

func (s *Server) resolveLogPath(raw string) (string, error) {
	defaultPath := "/root/robot.log"
	if _, err := os.Stat(defaultPath); err != nil {
		defaultPath = filepath.Join(s.cfg.ConfigDir, "log_robot")
	}
	if strings.TrimSpace(raw) == "" {
		return defaultPath, nil
	}
	want, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	allowed := []string{defaultPath, filepath.Join(s.cfg.ConfigDir, "log_robot")}
	for _, path := range allowed {
		abs, err := filepath.Abs(filepath.Clean(path))
		if err == nil && want == abs {
			return want, nil
		}
	}
	return "", fmt.Errorf("log path is not allowed")
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
<h2>TW Robot</h2><input name="password" type="password" placeholder="Web 閻庨潧妫涢悥? autofocus>
<button>闁谎嗩嚙缂?/button>{{if .Error}}<div class="err">閻庨潧妫涢悥婊堟煥濞嗘帩鍤?/div>{{end}}</form></body></html>`))

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>TW Robot Web</title><style>
:root{color-scheme:light;--bg:#eef2f6;--panel:#fff;--line:#d7dee8;--line2:#edf1f5;--text:#17202a;--muted:#64748b;--accent:#2563eb;--accent2:#1d4ed8;--danger:#b91c1c;--ok:#15803d;--warn:#a16207}
*{box-sizing:border-box}body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",Arial,sans-serif;margin:0;background:var(--bg);color:var(--text);font-size:14px}
header{height:48px;display:flex;gap:16px;align-items:center;padding:0 16px;background:#17202a;color:#fff}header b{font-size:16px}header span{opacity:.86}header a{color:#fff;text-decoration:none}.muted{color:var(--muted)}
.wrap{padding:12px 14px}.statusline{display:grid;grid-template-columns:repeat(6,minmax(130px,1fr));gap:8px;margin-bottom:10px}.metric{background:var(--panel);border:1px solid var(--line);border-radius:7px;padding:9px 10px}.metric .k{font-size:12px;color:var(--muted);margin-bottom:3px}.metric .v{font-size:18px;font-weight:700}.metric.good .v{color:var(--ok)}.metric.bad .v{color:var(--danger)}
.toolbar{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:10px}.toolbar .spacer{flex:1}.toolbar label{display:flex;gap:5px;align-items:center;color:var(--muted)}
button{height:32px;padding:0 11px;border:1px solid #b8c2d0;background:#fff;border-radius:6px;cursor:pointer;color:var(--text);font:inherit}button:hover{border-color:#8fa0b8;background:#f8fafc}button:disabled{opacity:.55;cursor:not-allowed}button.primary{background:var(--accent);border-color:var(--accent);color:#fff}button.primary:hover{background:var(--accent2)}button.danger{border-color:#efb5b5;color:var(--danger)}button.ghost{background:transparent}
input,textarea{font:inherit;border:1px solid #b8c2d0;border-radius:6px;padding:7px;background:#fff}input[type=number]{width:92px}.grid{display:grid;grid-template-rows:minmax(280px,1fr) 210px;gap:10px;height:calc(100vh - 150px)}
.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}.panelhead{height:36px;display:flex;align-items:center;gap:10px;padding:0 10px;background:#f8fafc;border-bottom:1px solid var(--line)}.panelhead h3{margin:0;font-size:14px}.panelhead span{color:var(--muted);font-size:12px}.sched{background:var(--panel);border:1px solid var(--line);border-radius:8px;margin-bottom:10px;padding:10px}.schedgrid{display:grid;grid-template-columns:repeat(6,minmax(120px,1fr));gap:8px}.scheditem{border:1px solid var(--line2);border-radius:6px;padding:7px 8px;background:#fbfdff}.scheditem .k{font-size:12px;color:var(--muted)}.scheditem .v{font-weight:700;margin-top:2px}.schedreason{margin-top:8px;color:var(--muted);font-size:12px}
.tablebox{height:calc(100% - 36px);overflow:auto}table{width:100%;border-collapse:collapse}th,td{padding:7px 8px;border-bottom:1px solid var(--line2);text-align:center;white-space:nowrap}th{background:#f8fafc;position:sticky;top:0;z-index:2;color:#475569;font-size:12px}tbody tr:nth-child(even){background:#fbfdff}tbody tr:hover{background:#eff6ff}tr.sel{background:#dbeafe!important}
pre{margin:0;padding:10px;height:174px;overflow:auto;white-space:pre-wrap;font-family:Consolas,"Cascadia Mono",monospace;font-size:12px;line-height:1.45;background:#0f172a;color:#dbeafe}.toast{position:fixed;right:14px;bottom:14px;max-width:520px;background:#17202a;color:#fff;border-radius:7px;padding:10px 12px;box-shadow:0 12px 32px rgba(15,23,42,.22);display:none}
dialog{border:1px solid var(--line);border-radius:8px;padding:0;min-width:460px;max-width:min(920px,96vw);box-shadow:0 18px 70px rgba(15,23,42,.22)}dialog::backdrop{background:rgba(15,23,42,.32)}.dlghead{display:flex;align-items:center;justify-content:space-between;padding:12px 14px;border-bottom:1px solid var(--line);background:#f8fafc}.dlghead h3{margin:0;font-size:15px}.dlgbody{padding:14px}.dlgfoot{display:flex;justify-content:flex-end;gap:8px;padding:12px 14px;border-top:1px solid var(--line);background:#f8fafc}.formgrid{display:grid;grid-template-columns:160px 1fr;gap:10px 12px;align-items:center}.formgrid label{color:#475569}.configbox{width:min(860px,88vw);height:min(560px,70vh);font-family:Consolas,"Cascadia Mono",monospace;font-size:12px;line-height:1.45}.chk{display:flex;gap:7px;align-items:center}.chk input{width:auto}.pill{display:inline-block;padding:2px 7px;border-radius:999px;background:#e2e8f0;color:#334155;font-size:12px}.pill.ok{background:#dcfce7;color:#166534}.pill.warn{background:#fef3c7;color:#92400e}.pill.bad{background:#fee2e2;color:#991b1b}
@media(max-width:900px){.statusline{grid-template-columns:repeat(2,1fr)}.schedgrid{grid-template-columns:repeat(2,1fr)}.grid{height:auto;grid-template-rows:420px 220px}.toolbar .spacer{display:none}}
</style></head><body>
<header><b>TW Robot Web</b><span>Web {{.WebAddr}}</span><span>TCP {{.RobotAddr}}</span><span id="gameHead">Game: -</span><a href="/logout">闂侇偀鍋撻柛?/a></header>
<div class="wrap">
<div class="statusline">
<div class="metric" id="mRobot" title="robot 閺夆晜绋撻埢?CPU闁靛棔绀侀崬瀵糕偓娑櫭幏?goroutine 闁?><div class="k">缂侇垵宕电划娲础閻樺灚鏆?/div><div class="v">婵☆偀鍋撴繛鏉戭儎閼?/div></div>
<div class="metric" id="mGame" title="婵☆偀鍋撴繛?10011 婵炴挸鎲￠崹娆戠博椤栨艾缍撻柨娑欑〒椤忣剟宕ｉ敐鍛；闁衡偓閹勫€甸悹鍥嚙瑜板洩銇愰幘鍐差枀濡増鍨挎禍?cfg 闁?max_user_num"><div class="k">婵炴挸鎲￠崹娆戠博椤栨艾缍?/div><div class="v">婵☆偀鍋撴繛鏉戭儎閼?/div></div>
<div class="metric" id="mKey" title="婵☆偀鍋撻柡?game 闁烩晩鍠栫紞?privatekey.pem / publickey.pem"><div class="k">Key 闁绘鍩栭埀?/div><div class="v">婵☆偀鍋撴繛鏉戭儎閼?/div></div>
<div class="metric"><div class="k">闁煎浜滄慨鈺佄熼垾宕囩</div><div class="v" id="mAuto">-</div></div>
<div class="metric"><div class="k">閺夆晜鍔橀、?闁烩晩鍠楅悥?/div><div class="v" id="mOnline">-</div></div>
<div class="metric"><div class="k">闁硅棄妫欓幉?/div><div class="v" id="mStore">-</div></div>
</div>
<div class="toolbar">
<button class="primary" data-key-action="1" onclick="refreshAll()">闁告帡鏀遍弻?/button><button data-key-action="1" onclick="openAutoDialog()">闁煎浜滄慨鈺冩偘鐏炶壈绀?/button><button data-key-action="1" onclick="openOnlineDialog()">濞戞挸锕﹂崵?/button><button data-key-action="1" onclick="runAction('robotsMove')">缂佸顕ф慨?/button><button data-key-action="1" onclick="runAction('robotsShout')">闁哥姴锕ㄩ惁?/button><button data-key-action="1" onclick="runAction('robotsStoreAsync')">闁硅棄妫欓幉?/button><button data-key-action="1" onclick="runAction('robotsLogoutAsync')">濞戞挸顑囬崵?/button><button data-key-action="1" class="danger" onclick="openCleanupDialog()">婵炴挸鎳愰幃?/button><button id="keyButton" onclick="openKeyDialog()">Key</button><button data-key-action="1" onclick="openConfigDialog()">闂佹澘绉堕悿?/button><button data-key-action="1" onclick="loadLog()">闁哄啨鍎辩换?/button>
<span class="spacer"></span><label class="chk"><input id="autoRefresh" type="checkbox" checked onchange="toggleAutoRefresh()">闁煎浜滄慨鈺呭礆闁垮鐓€</label><label>闁哄牜浜埀顒€顦板鍌炲箼瀹ュ嫮绋婇柡浣峰嵆閸?<input id="actionCount" type="number" min="1" max="500" value="1"></label><span class="muted" id="selectedInfo">鐎瑰憡鐓￠埀?0</span>
</div>
<div class="sched">
<div class="schedgrid">
<div class="scheditem"><div class="k">Policy</div><div class="v" id="sMode">-</div></div>
<div class="scheditem"><div class="k">Online</div><div class="v" id="sOnline">-</div></div>
<div class="scheditem"><div class="k">Store</div><div class="v" id="sStore">-</div></div>
<div class="scheditem"><div class="k">Scale</div><div class="v" id="sScale">-</div></div>
<div class="scheditem"><div class="k">Pressure</div><div class="v" id="sPressure">-</div></div>
<div class="scheditem"><div class="k">Release</div><div class="v" id="sRelease">-</div></div>
</div>
<div class="schedreason" id="sReason">-</div>
</div>
<div class="grid">
<div class="panel"><div class="panelhead"><h3>闁稿娲ｅЧ澶愬礆濡ゅ嫨鈧?/h3><span id="listInfo">缂佹稑顦欢鐔煎礆闁垮鐓€</span></div><div class="tablebox"><table id="tbl"><thead><tr>
<th><input type="checkbox" id="checkAll" onchange="toggleAll(this.checked)"></th><th>UID</th><th>CID</th><th>閻熸瑦甯熸竟濠囧触?/th><th>閻犳劧绠戣ぐ?/th><th>闁绘鍩栭埀?/th><th>缂侇偉顕ч悗?/th><th>缂佹稑顦辨?/th><th>闁煎崬濂旂粭?/th><th>闁糕晛閰ｉ弲?/th><th>闁告牕鎼悡?/th><th>X</th><th>Y</th><th>閻庢稒蓱濡?/th><th>闂佹彃绉风换?/th><th>闁硅棄妫欓幉?/th>
</tr></thead><tbody></tbody></table></div></div>
<div class="panel"><div class="panelhead"><h3>闁哄啨鍎辩换?/h3><span id="logInfo">闁告稒鍨濋幎銈嗘綇閹惧啿姣夐柛?robot 闁哄啨鍎辩换?/span></div><pre id="log"></pre></div>
</div></div>
<dialog id="modal"><div class="dlghead"><h3 id="modalTitle"></h3><button class="ghost" onclick="closeModal()">闁稿繑濞婂Λ?/button></div><div class="dlgbody" id="modalBody"></div><div class="dlgfoot" id="modalFoot"></div></dialog>
<div class="toast" id="toast"></div>
<script>
const selected = new Set(); let currentRows = []; let busy = false; let keyBlocked = false; let autoRefreshTimer = null;
function byId(id){return document.getElementById(id)}
function text(v){return String(v ?? '')}
function escapeHTML(v){return text(v).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function toast(msg){const t=byId('toast');t.textContent=msg;t.style.display='block';clearTimeout(t._timer);t._timer=setTimeout(()=>t.style.display='none',3600)}
function actionName(cmd){return ({robotsOnlineAsync:'濞戞挸锕﹂崵?,robotsStoreAsync:'闁硅棄妫欓幉?,robotsLogoutAsync:'濞戞挸顑囬崵?,cleanupRobotsAsync:'婵炴挸鎳愰幃?,robotsMove:'缂佸顕ф慨?,robotsShout:'闁哥姴锕ㄩ惁?}[cmd]||cmd)}
function applyKeyGate(){document.querySelectorAll('[data-key-action="1"]').forEach(b=>{b.disabled=busy||keyBlocked});const kb=byId('keyButton');if(kb)kb.disabled=busy}
function setBusy(v){busy=v;document.querySelectorAll('button').forEach(b=>{b.disabled=v&&!b.classList.contains('ghost')});applyKeyGate()}
async function api(command,payload={}){const r=await fetch('/api/call',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({command,payload})});const x=await r.json().catch(()=>({ok:false,error:'invalid json'}));if(!r.ok||!x.ok)throw new Error(x.error||('HTTP '+r.status));return x}
function resultOf(x){return x&&x.result&&x.result.result?x.result.result:x.result}
function writeLog(v){byId('log').textContent=typeof v==='string'?v:JSON.stringify(v,null,2)}
function uptime(sec){sec=Number(sec||0);const h=Math.floor(sec/3600),m=Math.floor((sec%3600)/60),s=sec%60;return h?(h+':'+String(m).padStart(2,'0')+':'+String(s).padStart(2,'0')):(m+':'+String(s).padStart(2,'0'))}
function selectedPayload(){const u=[...selected].map(Number);if(u.length)return {uids:u};return {count:Math.max(1,Number(byId('actionCount').value||1))}}
function updateSelectedInfo(){byId('selectedInfo').textContent='鐎瑰憡鐓￠埀?'+selected.size;byId('checkAll').checked=currentRows.length>0 && currentRows.every(r=>selected.has(String(r.uid||'')))}
async function guarded(name,fn){if(busy)return;setBusy(true);try{const out=await fn();if(out!==undefined)writeLog(out);const queued=JSON.stringify(out||{}).includes('"state":"queued"');toast(name+(queued?' 鐎圭寮惰ぐ浣圭閵堝懏鍊甸柛娆戝婢х晫鎮?:' 閻庣懓鏈崹?))}catch(e){writeLog({ok:false,error:e.message});toast(name+' 濠㈡儼绮剧憴? '+e.message)}finally{setBusy(false)}}
async function refreshAll(){await guarded('闁告帡鏀遍弻?,async()=>{await Promise.all([refreshStatus(),refreshScheduler(),refreshRobots(),checkGame(),refreshKeypair()]);return '闁告帡鏀遍弻濠勨偓鐟版湰閸?'+new Date().toLocaleTimeString()})}
async function refreshQuiet(){if(busy||!byId('autoRefresh')?.checked)return;try{await Promise.all([refreshStatus(),refreshScheduler(),refreshRobots(),checkGame(),refreshKeypair()])}catch(e){}}
async function refreshStatus(){const x=await api('autoStatus');let r=resultOf(x);if(r&&r.result)r=r.result;if(r){byId('mAuto').textContent=r.enabled?'鐎殿喒鍋撻柛?:'闁稿繑濞婂Λ?;byId('mOnline').textContent=(r.running||0)+' / '+(r.target_online||0);byId('mStore').textContent=String(r.store_running||0)}try{const sx=await api('systemStatus');let s=resultOf(sx);if(s&&s.result)s=s.result;const el=byId('mRobot');el.className='metric good';const cpu=Number(s.robot_cpu_percent||0).toFixed(1);el.querySelector('.v').textContent=cpu+'% '+(s.robot_memory_mb||0)+'MB';el.querySelector('.k').textContent='缂侇垵宕电划娲础閻樺灚鏆?/ '+(s.robot_threads||0)+' goroutines'}catch(e){const el=byId('mRobot');el.className='metric bad';el.querySelector('.v').textContent='闂佹寧鐟ㄩ?}}
async function refreshScheduler(){try{const x=await api('schedulerStatus');let s=resultOf(x);if(s&&s.result)s=s.result;if(!s)return;byId('sMode').textContent=s.mode||'-';byId('sOnline').textContent=(s.online_start_rate||0)+'/s batch '+(s.online_batch_size||0);byId('sStore').textContent=(s.store_running||0)+'/'+(s.store_concurrent||0)+' p'+(s.store_probability_percent||0)+'%';byId('sScale').textContent='up '+(s.scale_up_batch||0)+' down '+(s.scale_down_batch||0);byId('sPressure').textContent='login '+(s.connecting||0)+' cpu '+Number(s.cpu_percent||0).toFixed(1)+'%';byId('sRelease').textContent='br '+(s.breaker_release_batch||0)+' port '+(s.port_down_release_batch||0);byId('sReason').textContent=(s.reason||'-')+' | running '+(s.running||0)+'/'+(s.target_online||0)+' idle '+(s.idle||0)+' port '+(s.game_port_ready?'ready':'down')+' breaker '+(s.breaker_active?'on':'off')+' | actor i/a/o/r/b/rel '+(s.actor_idle||0)+'/'+(s.actor_assigned||0)+'/'+(s.actor_online||0)+'/'+(s.actor_running||0)+'/'+(s.actor_busy||0)+'/'+(s.actor_releasing||0)}catch(e){byId('sMode').textContent='error';byId('sReason').textContent=e.message}}
async function refreshRobots(){const x=await api('robotsStatus',{count:10000});const r=resultOf(x)||{};currentRows=r.robots||[];const tb=document.querySelector('#tbl tbody');tb.textContent='';for(const a of currentRows){const uid=String(a.uid||'');const tr=document.createElement('tr');if(selected.has(uid))tr.className='sel';const cb=document.createElement('input');cb.type='checkbox';cb.checked=selected.has(uid);appendCell(tr,cb);[uid,a.cid,a.name,a.account,a.state_name,a.robot_type,a.level,(text(a.job)+'/'+text(a.grow)),a.village,a.area,a.x,a.y,uptime(a.uptime_seconds),a.reconnects].forEach(v=>appendCell(tr,text(v)));const store=a.store_display_ack?'闁瑰瓨鍔曟慨?:((a.store_created||a.robot_type==2||a.robot_type==3)?'闁告垵妫楅ˇ?:'');const span=document.createElement('span');if(store){span.className='pill '+(a.store_display_ack?'ok':'warn');span.textContent=store}appendCell(tr,span);tr.onclick=e=>{if(e.target.tagName!=='INPUT')cb.checked=!cb.checked;cb.checked?selected.add(uid):selected.delete(uid);tr.className=selected.has(uid)?'sel':'';updateSelectedInfo()};tb.appendChild(tr)}byId('listInfo').textContent='闁?'+currentRows.length+' 濞戞搩浜风槐婵嬪嫉閳ь剟宕ユ惔鈥崇厱闁?'+new Date().toLocaleTimeString();updateSelectedInfo()}
function appendCell(tr,v){const td=document.createElement('td');if(v instanceof Node)td.appendChild(v);else td.textContent=v;tr.appendChild(td)}
async function checkGame(){const x=await fetch('/api/game-port').then(r=>r.json());byId('gameHead').textContent='Game: '+(x.ok?'open':'closed')+' '+(x.addr||'');byId('mGame').className='metric '+(x.ok?'good':'bad');let txt=x.ok?'鐎瑰憡褰冪槐鎴﹀绩?:'闁哄牜浜滅槐鎴﹀绩?;if(x.ok&&x.max_user_num)txt+=' / '+x.max_user_num;byId('mGame').querySelector('.v').textContent=txt;byId('mGame').title=x.game_cfg_name?('闂佹澘绉堕悿?'+x.game_cfg_name+'.cfg闁挎稑顔卆x_user_num='+x.max_user_num):'婵☆偀鍋撴繛?10011 婵炴挸鎲￠崹娆戠博椤栨艾缍撻柨娑欑〒椤忣剟宕ｉ敐鍛；闁衡偓閹勫€甸悹鍥嚙瑜板洩銇愰幘鍐差枀濡増鍨挎禍?cfg 闁?max_user_num'}
async function loadLog(){await guarded('閻犲洩顕цぐ鍥籍閵夈儳绠?,async()=>{const x=await fetch('/api/log').then(r=>r.json());return (x.path?x.path+'\n':'')+(x.text||'')})}
async function runAction(cmd){await guarded(actionName(cmd),async()=>{const x=await api(cmd,selectedPayload());setTimeout(()=>refreshAll(),800);return x})}
function toggleAll(checked){if(checked){currentRows.forEach(r=>selected.add(String(r.uid||'')))}else{selected.clear()}document.querySelectorAll('#tbl tbody tr').forEach(tr=>{const cb=tr.querySelector('input');if(cb)cb.checked=checked;tr.className=checked?'sel':''});updateSelectedInfo()}
function closeModal(){byId('modal').close()}
function showModal(title,body,foot){byId('modalTitle').textContent=title;byId('modalBody').innerHTML=body;byId('modalFoot').innerHTML=foot||'<button onclick="closeModal()">闁稿繑濞婂Λ?/button>';byId('modal').showModal()}
function openOnlineDialog(){if(selected.size){runAction('robotsOnlineAsync');return}showModal('闁归潧顑呮慨鈺傜▔婵犲嫬娈?,'<div class="formgrid"><label>濞戞挸锕﹂崵搴ㄥ极娴兼潙娅?/label><input id="onlineCount" type="number" min="1" max="500" value="10"></div>','<button onclick="closeModal()">闁告瑦鐗楃粔?/button><button class="primary" onclick="submitOnline()">濞戞挸锕﹂崵?/button>')}
async function submitOnline(){const c=Math.max(1,Number(byId('onlineCount').value||1));closeModal();await guarded('濞戞挸锕﹂崵?,async()=>{const x=await api('robotsOnlineAsync',{count:c});setTimeout(()=>refreshAll(),1000);return x})}
function openCleanupDialog(){const target=selected.size?('闂侇偄顦懙鎴︽儍?'+selected.size+' 濞戞搩浜滄禍锝嗙?):'闁稿繈鍔戦崕?robot 闁稿娲ｅЧ?;showModal('婵炴挸鎳愰幃濠囧磻閸ワ附鐪?,'<p>閻忓繐妫楅崹褰掓⒔?'+escapeHTML(target)+'闁靛棔绔eanupRobots 濞村吋纰嶇€?registry闁靛棔娴囨径鍕矗?UID闁靛棔绗島mmylist 闁哄鈧弶顐介梻鍕姇閸╂鏁嶅畝鍕級闁稿繐绉烽銈夊礆閻樿櫕鐝梺顐ｄ亢椤鎳濈仦鍌楀亾?/p>','<button onclick="closeModal()">闁告瑦鐗楃粔?/button><button class="danger" onclick="submitCleanup()">缁绢収鍠涢璇层€掗崨顖涘€?/button>')}
async function submitCleanup(){const p=selected.size?{uids:[...selected].map(Number),force:true}:{force:true};closeModal();await guarded('婵炴挸鎳愰幃?,async()=>{const x=await api('cleanupRobotsAsync',p);selected.clear();setTimeout(()=>refreshAll(),1000);return x})}
async function openAutoDialog(){await guarded('read auto config',async()=>{const x=await api('robotConfigGet');const cfg=(resultOf(x)||{}).config||{};showModal('Auto', '<div class="formgrid"><label>Enabled</label><label class="chk"><input id="autoEnabled" type="checkbox" '+(String(cfg.auto_actions)==='true'?'checked':'')+'>On</label><label>Target online</label><input id="autoTarget" type="number" min="1" max="1000" value="'+escapeHTML(cfg.auto_target_online_count||600)+'"><div class="muted" style="grid-column:1/-1">Scheduler derives pacing, store windows, breaker and pressure release automatically.</div></div>','<button onclick="closeModal()">Cancel</button><button onclick="submitAuto(false)">Save and stop</button><button class="primary" onclick="submitAuto(true)">Save and start</button>');return undefined})}
async function submitAuto(forceStart){const enabled=forceStart||byId('autoEnabled').checked;const updates={'auto.auto_target_online_count':String(byId('autoTarget').value||600),'auto.auto_actions':String(enabled)};closeModal();await guarded('Auto',async()=>{const a=await api('robotConfigUpdate',{updates});const b=await api(enabled?'autoStart':'autoStop');setTimeout(()=>refreshAll(),1000);return {config:a,auto:b}})}
async function refreshKeypair(){try{const x=await api('keypairStatus');const r=resultOf(x)||{};const el=byId('mKey');keyBlocked=!(r.key_state==='default'||r.key_state==='user'||r.game_valid);applyKeyGate();el.className='metric '+(keyBlocked?'bad':'good');el.querySelector('.k').textContent='Key 闁绘鍩栭埀?;el.querySelector('.v').textContent=r.key_state_label||r.key_state||'缂傚倸鎼妵?}catch(e){keyBlocked=true;applyKeyGate();const el=byId('mKey');el.className='metric bad';el.querySelector('.v').textContent='婵☆偀鍋撴繛鏉戭儏閵囨垹鎷?}}
async function openKeyDialog(){await guarded('Key',async()=>{const x=await api('keypairStatus');const r=resultOf(x)||{};const rows=[['闁绘鍩栭埀?,r.key_state_label||r.key_state||'缂傚倸鎼妵?],['闁告鍠庡ú?,r.key_reason||'-'],['game 缂佸绶氶幐?,r.game_private||'-'],['game 闁稿浚鍓熼幐?,r.game_public||'-'],['闁圭娲ㄥЧ?,r.fingerprint||'-']];let html='<div class="formgrid">';for(const row of rows){html+='<label>'+escapeHTML(row[0])+'</label><div>'+escapeHTML(row[1])+'</div>'}html+='</div>';html+=r.game_valid?'<p class="muted">鐟滅増鎸告晶?game 闁烩晩鍠栫紞宥団偓闈涙閹告粎鈧數鎳撻幃搴♀枖閺囶亞骞㈠☉鎾愁儓濞村洭鎯冮崟顒佇︾憸鐗堟尭婢х姴顔忛弻銉у矗閻犲洣鑳跺▓鎴犫偓闈涙閹告粎鈧潧绠嶉埀?/p>':'<p class="muted">閻犲洤鍢插﹢?game 闁烩晩鍠栫紞宥夋煀瀹ュ洨鏋傞柛姘墛绾?RSA 閻庨潧妫濋幐婊呪偓鐢垫缁辨繈骞嬮弽顑藉亾閸涱垰浠柛鎴濐煼閸ｆ挳寮ㄦィ鍐笡閻?Key闁?/p>';const releaseDis=(r.can_release_default&&r.default_valid)?'':' disabled';const downloadDis=r.can_download_current?'':' disabled';showModal('RSA Key 缂佺媴绱曢幃?,html,'<button onclick="closeModal()">闁稿繑濞婂Λ?/button><button'+downloadDis+' onclick="downloadCurrentKeypair()">濞戞挸顑堝ù鍥亹閹惧啿顤?Key</button><button class="primary"'+releaseDis+' onclick="releaseDefaultKeypair()">闂佹彃锕ラ弬浣诡渶濡鍚?Key</button>');return undefined})}
async function releaseDefaultKeypair(){closeModal();await guarded('闂佹彃锕ラ弬浣诡渶濡鍚?Key',async()=>{const x=await api('keypairReleaseDefault',{});await refreshKeypair();return x})}
function downloadCurrentKeypair(){window.location='/api/keypair-download'}
async function openConfigDialog(){await guarded('閻犲洩顕цぐ鍥煀瀹ュ洨鏋?,async()=>{const x=await api('robotConfigGet');const body='<textarea id="cfgText" class="configbox"></textarea>';showModal('robot_config.ini',body,'<button onclick="closeModal()">闁告瑦鐗楃粔?/button><button class="primary" onclick="saveConfig()">濞ｅ洦绻傞悺?/button>');byId('cfgText').value=(resultOf(x)||{}).text||'';return undefined})}
async function saveConfig(){const text=byId('cfgText').value;closeModal();await guarded('濞ｅ洦绻傞悺銊╂煀瀹ュ洨鏋?,async()=>{const x=await api('robotConfigUpdate',{text});setTimeout(()=>refreshAll(),1000);return x})}
function toggleAutoRefresh(){if(autoRefreshTimer){clearInterval(autoRefreshTimer);autoRefreshTimer=null}if(byId('autoRefresh')?.checked){autoRefreshTimer=setInterval(refreshQuiet,8000)}}
refreshAll();toggleAutoRefresh();
</script></body></html>`))
