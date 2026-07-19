package webadmin

import (
	"bufio"
	"bytes"
	"crypto/sha256"
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
	"robot/internal/capability/catalog"
	"robot/internal/capability/keypair"
	"robot/internal/capability/robotconfig"
	"robot/internal/foundation/config"
	"robot/internal/foundation/dbstatus"
	"robot/internal/shared"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

type diagnosticsReport struct {
	OK        bool                 `json:"ok"`
	Generated string               `json:"generated_at"`
	Summary   diagnosticsSummary   `json:"summary"`
	Sections  []diagnosticsSection `json:"sections"`
}

type diagnosticsSummary struct {
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

type diagnosticsSection struct {
	Name   string             `json:"name"`
	Status string             `json:"status"`
	Checks []diagnosticsCheck `json:"checks"`
}

var partySkillErrorLogPatterns = []string{
	"PARTY_DUNGEON_SKILL_PROFILE_ERROR",
	"PARTY_DUNGEON_SKILL_CAST_ERROR",
	"PARTY_DUNGEON_SKILL_RECOVER_ERROR",
}

type diagnosticsCheck struct {
	Name     string                 `json:"name"`
	Status   string                 `json:"status"`
	Message  string                 `json:"message"`
	Expected interface{}            `json:"expected,omitempty"`
	Observed interface{}            `json:"observed,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

type diagnosticsBuilder struct {
	cfg    *config.SysConfig
	server *Server
	report diagnosticsReport
}

const (
	diagOK    = "ok"
	diagWarn  = "warn"
	diagError = "error"

	diagnosticsDFGameRJSPath     = "/dp2/df_game_r.js"
	diagnosticsAuctionGuardBegin = "// DP2_AUCTION_SEARCH_HOOK_GUARD_BEGIN"
	diagnosticsAuctionGuardEnd   = "// DP2_AUCTION_SEARCH_HOOK_GUARD_END"
)

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.buildDiagnostics())
}

func (s *Server) buildDiagnostics() diagnosticsReport {
	cfg := s.cfg
	if disk, err := s.loadDiskConfig(); err == nil {
		cfg = disk
	}
	b := diagnosticsBuilder{
		cfg:    cfg,
		server: s,
		report: diagnosticsReport{
			OK:        true,
			Generated: time.Now().Format(time.RFC3339),
		},
	}
	b.addRuntimeSection()
	b.addDatabaseSection()
	b.addFileSection()
	b.addMarketSection()
	b.addPartySection()
	b.addSkillSection()
	b.addLogSection()
	for _, section := range b.report.Sections {
		for _, check := range section.Checks {
			switch check.Status {
			case diagError:
				b.report.Summary.Errors++
			case diagWarn:
				b.report.Summary.Warnings++
			default:
				b.report.Summary.OK++
			}
		}
	}
	b.report.OK = b.report.Summary.Errors == 0
	return b.report
}

func (b *diagnosticsBuilder) addSection(name string, checks ...diagnosticsCheck) {
	status := diagOK
	for _, check := range checks {
		if check.Status == diagError {
			status = diagError
			break
		}
		if check.Status == diagWarn {
			status = diagWarn
		}
	}
	b.report.Sections = append(b.report.Sections, diagnosticsSection{Name: name, Status: status, Checks: checks})
}

func (b *diagnosticsBuilder) addRuntimeSection() {
	cfg := b.cfg
	checks := []diagnosticsCheck{
		buildInfoCheck(),
		fileCheck("robot binary", executablePath(), true),
		fileCheck("df_game_r binary", cfg.DFGameR, true),
	}
	ports := expectedProcessPorts(cfg)
	ss, ssErr := listeningProcessPorts()
	if ssErr != nil {
		checks = append(checks, diagnosticsCheck{Name: "listening ports", Status: diagWarn, Message: ssErr.Error()})
	} else {
		for _, p := range ports {
			entry := ss[p.port]
			status := diagError
			msg := "port is not listening"
			if entry.Port == p.port {
				status = diagOK
				msg = "port is listening"
				if p.process != "" && !strings.Contains(entry.Process, p.process) {
					status = diagWarn
					msg = "port is listening, but process name is unexpected"
				}
			}
			checks = append(checks, diagnosticsCheck{
				Name:     p.name + " port",
				Status:   status,
				Message:  msg,
				Expected: map[string]interface{}{"port": p.port, "process": p.process},
				Observed: entry,
			})
		}
		checks = append(checks, actualServicePortsCheck(ss))
	}
	if raw, err := callRobot(b.server.robotAddr, "systemStatus", nil, 5*time.Second, b.cfg.MaxResponseBytes); err == nil {
		checks = append(checks, diagnosticsCheck{Name: "robot api systemStatus", Status: diagOK, Message: "robot API responded", Observed: parseRobotResult(raw)})
	} else {
		checks = append(checks, diagnosticsCheck{Name: "robot api systemStatus", Status: diagError, Message: err.Error(), Expected: b.server.robotAddr})
	}
	st := keypair.BuildKeypairStatus(cfg)
	keyStatus := diagOK
	keyMsg := "game keypair is valid"
	if !st.GameValid {
		keyStatus = diagError
		keyMsg = st.Error
		if strings.TrimSpace(keyMsg) == "" {
			keyMsg = st.KeyReason
		}
	}
	checks = append(checks, diagnosticsCheck{Name: "RSA keypair", Status: keyStatus, Message: keyMsg, Observed: st})
	b.addSection("Runtime / Ports", checks...)
}

func (b *diagnosticsBuilder) addDatabaseSection() {
	checks := []diagnosticsCheck{}
	report := dbstatus.CheckStructure(b.cfg, requiredDBSchemas(), requiredDBTables())
	if !report.Connect.OK {
		b.addSection("Database", diagnosticsCheck{Name: "connect", Status: diagError, Message: report.Connect.Error, Expected: report.Target, Observed: report.Connect})
		return
	}
	checks = append(checks, diagnosticsCheck{Name: "connect", Status: diagOK, Message: "database connection ok", Observed: report.Connect})
	for _, schema := range report.Schemas {
		checks = append(checks, boolCheck("schema "+schema.Schema, schema.Exists, stringErr(schema.Error), "schema exists", "schema is missing", schema))
	}
	for _, table := range report.Tables {
		status := diagOK
		msg := "required columns exist"
		if table.Error != "" {
			status = diagError
			msg = table.Error
		} else if !table.Exists {
			status = diagError
			msg = "missing columns: " + strings.Join(table.Missing, ",")
		}
		checks = append(checks, diagnosticsCheck{
			Name:     table.Schema + "." + table.Table,
			Status:   status,
			Message:  msg,
			Observed: table,
		})
	}
	b.addSection("Database", checks...)
}

func (b *diagnosticsBuilder) addFileSection() {
	configDir := b.cfg.ConfigDir
	gameDir := filepath.Dir(b.cfg.DFGameR)
	paths := []struct {
		name     string
		path     string
		required bool
	}{
		{"config.ini", filepath.Join(configDir, "config.ini"), true},
		{"robot_config.ini", filepath.Join(configDir, "robot_config.ini"), true},
		{"Script.pvf", filepath.Join(gameDir, "Script.pvf"), true},
		{"pvf_manifest.json", filepath.Join(configDir, "pvf_manifest.json"), true},
		{"pvf_equipment_catalog.json", filepath.Join(configDir, "pvf_equipment_catalog.json"), true},
		{"pvf_stackable_catalog.json", filepath.Join(configDir, "pvf_stackable_catalog.json"), true},
		{"pvf_map_catalog.json", filepath.Join(configDir, "pvf_map_catalog.json"), true},
		{"pvf_skill_state_catalog.json", filepath.Join(configDir, "pvf_skill_state_catalog.json"), true},
		{"pvf_iteminfo.dat", filepath.Join(configDir, "pvf_iteminfo.dat"), true},
		{"auction iteminfo.dat", "/home/neople/auction/iteminfo.dat", true},
		{"point iteminfo.dat", "/home/neople/point/iteminfo.dat", true},
	}
	checks := make([]diagnosticsCheck, 0, len(paths)+3)
	for _, p := range paths {
		checks = append(checks, fileCheck(p.name, p.path, p.required))
	}
	checks = append(checks, b.pvfManifestCheck())
	checks = append(checks, compareFileHashCheck("auction iteminfo matches pvf export", filepath.Join(configDir, "pvf_iteminfo.dat"), "/home/neople/auction/iteminfo.dat"))
	checks = append(checks, compareFileHashCheck("point iteminfo matches pvf export", filepath.Join(configDir, "pvf_iteminfo.dat"), "/home/neople/point/iteminfo.dat"))
	b.addSection("Files / PVF / ItemInfo", checks...)
}

func (b *diagnosticsBuilder) addMarketSection() {
	checks := []diagnosticsCheck{}
	if raw, err := callRobot(b.server.robotAddr, "marketStatus", nil, 8*time.Second, b.cfg.MaxResponseBytes); err == nil {
		status := parseRobotResult(raw)
		checks = append(checks, diagnosticsCheck{Name: "marketStatus", Status: diagOK, Message: "market API responded", Observed: status})
		checks = append(checks, marketStatusChecks(status)...)
	} else {
		checks = append(checks, diagnosticsCheck{Name: "marketStatus", Status: diagWarn, Message: err.Error()})
	}
	checks = append(checks, auctionGuardCheck(diagnosticsDFGameRJSPath))
	checks = append(checks, auctionMemoryPatchReadOnlyCheck())
	b.addSection("Market", checks...)
}

func (b *diagnosticsBuilder) addPartySection() {
	cfg, err := b.server.loadPartyCompatConfig()
	if err != nil {
		b.addSection("Party", diagnosticsCheck{Name: "party compat config", Status: diagError, Message: err.Error()})
		return
	}
	status := inspectPartyCompat(b.cfg.RobotGamePort, cfg)
	status.DesiredEnabled = cfg.Enabled
	checks := []diagnosticsCheck{
		{
			Name:     "party compatibility patch",
			Status:   partyCompatDiagStatus(status),
			Message:  partyCompatDiagMessage(status),
			Expected: map[string]interface{}{"desired_enabled": cfg.Enabled, "account_start": cfg.AccountStart, "account_end": cfg.AccountEnd},
			Observed: status,
		},
		portDialCheck("relay service", "127.0.0.1", b.cfg.RelayPort),
		udpListeningCheck("party route0 UDP", b.cfg.PartyRoute0Port),
	}
	checks = append(checks, partyAccountRangeCheck(b.cfg.ConfigDir, status.AccountStart, status.AccountEnd))
	b.addSection("Party", checks...)
}

func partyAccountRangeCheck(configDir string, patchStart, patchEnd uint32) diagnosticsCheck {
	path := filepath.Join(configDir, "robot_config.ini")
	rc, err := robotconfig.LoadFile(path)
	if err != nil {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "cannot load robot account range: " + err.Error(),
			Expected: path,
		}
	}

	observed := map[string]interface{}{
		"robot_uid_start":     rc.RobotUIDStart,
		"robot_uid_end":       rc.RobotUIDEnd,
		"patch_start":         patchStart,
		"patch_end_exclusive": patchEnd,
	}
	expectedStart, expectedEnd, ok := partyCompatConfiguredWindow(rc.RobotUIDStart, rc.RobotUIDEnd)
	if !ok {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "configured robot account range is invalid",
			Observed: observed,
		}
	}
	covered := patchStart <= expectedStart && patchEnd >= expectedEnd
	if !covered {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "party patch range does not cover the configured party account window",
			Expected: map[string]interface{}{"patch_start_lte": expectedStart, "patch_end_exclusive_gte": expectedEnd},
			Observed: observed,
		}
	}
	return diagnosticsCheck{
		Name:     "party account range",
		Status:   diagOK,
		Message:  "party patch range covers the configured party account window",
		Observed: observed,
	}
}

func (b *diagnosticsBuilder) addSkillSection() {
	configDir := b.cfg.ConfigDir
	checks := skillDiagnosticsChecks(configDir)
	checks = append(checks, recentLogPatternCheck("recent party skill errors", filepath.Join(configDir, "log_robot"), partySkillErrorLogPatterns))
	b.addSection("Skill", checks...)
}

func (b *diagnosticsBuilder) addLogSection() {
	limit := int64(b.cfg.LogMaxSizeMB) * 1024 * 1024
	if limit <= 0 {
		limit = 100 * 1024 * 1024
	}
	checks := []diagnosticsCheck{}
	for _, path := range []string{
		filepath.Join(b.cfg.ConfigDir, "log_robot"),
		filepath.Join(b.cfg.ConfigDir, "robot_stdout.log"),
		filepath.Join(b.cfg.ConfigDir, "robot_start_error.log"),
		filepath.Join(b.cfg.ConfigDir, "market_log.jsonl"),
	} {
		checks = append(checks, logSizeCheck(path, limit))
	}
	checks = append(checks, recentLogPatternCheck("recent fatal log keywords", filepath.Join(b.cfg.ConfigDir, "log_robot"), []string{"panic", "fatal", "too many open files", "cannot assign requested address", "message_queue_full", "timer_queue_overflow"}))
	b.addSection("Logs", checks...)
}

func requiredDBSchemas() []string {
	return []string{"d_taiwan", "taiwan_cain", "taiwan_cain_2nd", "taiwan_login", "taiwan_billing", "d_starsky", "taiwan_cain_auction_gold", "taiwan_cain_auction_cera"}
}

func requiredDBTables() []dbstatus.TableRequirement {
	return []dbstatus.TableRequirement{
		{Schema: "d_taiwan", Table: "accounts", Columns: []string{"UID", "accountname"}},
		{Schema: "taiwan_cain", Table: "charac_info", Columns: []string{"m_id", "charac_no", "charac_name", "job", "grow_type", "lev", "delete_flag"}},
		{Schema: "taiwan_cain", Table: "charac_stat", Columns: []string{"charac_no", "village"}},
		{Schema: "taiwan_cain", Table: "charac_view", Columns: []string{"m_id", "charac_no"}},
		{Schema: "taiwan_cain_2nd", Table: "inventory", Columns: []string{"charac_no", "inventory"}},
		{Schema: "taiwan_cain_2nd", Table: "skill", Columns: []string{"charac_no", "skill_slot", "skill_slot_2nd", "skill_command", "script_version"}},
		{Schema: "taiwan_cain_2nd", Table: "user_items", Columns: []string{"charac_no", "slot", "it_id"}},
		{Schema: "d_starsky", Table: "Dummylist", Columns: []string{"UID", "CID", "curvill", "curarea", "curx", "cury", "ip", "function_type", "discost"}},
		{Schema: "d_starsky", Table: "v4_ai_user", Columns: []string{"uid", "msg_state", "move_state"}},
		{Schema: "d_starsky", Table: "robot_registry", Columns: []string{"uid", "cid", "account", "charac_name", "created_at"}},
		{Schema: "d_starsky", Table: "Robot_stall", Columns: []string{"Trade_item", "price", "item_number", "function_type", "state", "UID"}},
		{Schema: "d_starsky", Table: "Robot_stall_config", Columns: []string{"cfg_content", "cfg_type", "UID", "function_type", "state"}},
		{Schema: "taiwan_cain_auction_gold", Table: "auction_main", Columns: []string{"auction_id", "owner_id"}},
		{Schema: "taiwan_cain_auction_cera", Table: "auction_main", Columns: []string{"auction_id", "owner_id"}},
	}
}

func stringErr(message string) error {
	if message == "" {
		return nil
	}
	return fmt.Errorf("%s", message)
}

type expectedPort struct {
	name    string
	port    int
	process string
}

func expectedProcessPorts(cfg *config.SysConfig) []expectedPort {
	return []expectedPort{
		{"Robot API", cfg.RobotPort, "robot"},
		{"Web", cfg.WebPort, "robot"},
		{"Game", cfg.RobotGamePort, "df_game_r"},
		{"Monitor", cfg.MonitorPort, "df_monitor_r"},
		{"Auction", cfg.AuctionPort, "df_auction_r"},
		{"Point", cfg.PointPort, "df_point_r"},
		{"Relay", cfg.RelayPort, "df_relay_r"},
	}
}

func buildInfoCheck() diagnosticsCheck {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return diagnosticsCheck{Name: "robot build info", Status: diagWarn, Message: "build info is unavailable"}
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs") || setting.Key == "GOOS" || setting.Key == "GOARCH" {
			settings[setting.Key] = setting.Value
		}
	}
	return diagnosticsCheck{Name: "robot build info", Status: diagOK, Message: "build info available", Observed: map[string]interface{}{"go": info.GoVersion, "module": info.Main.Path, "version": info.Main.Version, "settings": settings}}
}

type listeningPort struct {
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Line    string `json:"line,omitempty"`
}

func listeningProcessPorts() (map[int]listeningPort, error) {
	out, err := exec.Command("ss", "-lntp").Output()
	if err != nil {
		return nil, err
	}
	portRE := regexp.MustCompile(`[:.]([0-9]{1,5})\s`)
	procRE := regexp.MustCompile(`"([^"]+)",pid=([0-9]+)`)
	result := map[int]listeningPort{}
	for _, line := range strings.Split(string(out), "\n") {
		pm := portRE.FindStringSubmatch(line)
		if len(pm) != 2 {
			continue
		}
		port, _ := strconv.Atoi(pm[1])
		if port <= 0 {
			continue
		}
		entry := listeningPort{Port: port, Line: strings.TrimSpace(line)}
		if m := procRE.FindStringSubmatch(line); len(m) == 3 {
			entry.Process = m[1]
			entry.PID, _ = strconv.Atoi(m[2])
		}
		result[port] = entry
	}
	return result, nil
}

func actualServicePortsCheck(ports map[int]listeningPort) diagnosticsCheck {
	targets := []string{"df_game_r", "df_monitor_r", "df_auction_r", "df_point_r", "df_relay_r", "robot"}
	observed := map[string][]int{}
	for _, entry := range ports {
		for _, target := range targets {
			if strings.Contains(entry.Process, target) {
				observed[target] = append(observed[target], entry.Port)
			}
		}
	}
	for name := range observed {
		sort.Ints(observed[name])
	}
	status := diagOK
	msg := "service process ports collected"
	if len(observed) == 0 {
		status = diagWarn
		msg = "no known service process ports found"
	}
	return diagnosticsCheck{Name: "actual service ports", Status: status, Message: msg, Observed: observed}
}

func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

func fileCheck(name, path string, required bool) diagnosticsCheck {
	info, err := os.Stat(path)
	if err != nil {
		status := diagWarn
		if required {
			status = diagError
		}
		return diagnosticsCheck{Name: name, Status: status, Message: err.Error(), Expected: path}
	}
	return diagnosticsCheck{
		Name:    name,
		Status:  diagOK,
		Message: "file exists",
		Observed: map[string]interface{}{
			"path":     path,
			"size":     info.Size(),
			"mod_time": info.ModTime().Format(time.RFC3339),
		},
	}
}

func compareFileHashCheck(name, a, b string) diagnosticsCheck {
	ha, ia, errA := fileSHA256(a)
	hb, ib, errB := fileSHA256(b)
	if errA != nil || errB != nil {
		return diagnosticsCheck{Name: name, Status: diagError, Message: fmt.Sprintf("read failed: %v %v", errA, errB), Expected: []string{a, b}}
	}
	status := diagOK
	msg := "files match"
	if ha != hb || ia.Size() != ib.Size() {
		status = diagError
		msg = "files differ"
	}
	return diagnosticsCheck{Name: name, Status: status, Message: msg, Observed: map[string]interface{}{
		"source": map[string]interface{}{"path": a, "size": ia.Size(), "sha256": ha, "mod_time": ia.ModTime().Format(time.RFC3339)},
		"target": map[string]interface{}{"path": b, "size": ib.Size(), "sha256": hb, "mod_time": ib.ModTime().Format(time.RFC3339)},
	}}
}

func fileSHA256(path string) (string, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(h.Sum(nil)), info, nil
}

func (b *diagnosticsBuilder) pvfManifestCheck() diagnosticsCheck {
	manifestPath := filepath.Join(b.cfg.ConfigDir, "pvf_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return diagnosticsCheck{Name: "pvf manifest freshness", Status: diagError, Message: err.Error(), Expected: manifestPath}
	}
	var manifest struct {
		Version int    `json:"version"`
		Source  string `json:"source"`
		Size    int64  `json:"size"`
		ModTime int64  `json:"mod_time"`
		MD5     string `json:"md5"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return diagnosticsCheck{Name: "pvf manifest freshness", Status: diagError, Message: err.Error(), Expected: manifestPath}
	}
	info, err := os.Stat(manifest.Source)
	if err != nil {
		return diagnosticsCheck{Name: "pvf manifest freshness", Status: diagError, Message: err.Error(), Observed: manifest}
	}
	status := diagOK
	msg := "PVF export matches source metadata"
	if info.Size() != manifest.Size || info.ModTime().Unix() != manifest.ModTime {
		status = diagWarn
		msg = "PVF source metadata changed; export may be stale"
	}
	return diagnosticsCheck{Name: "pvf manifest freshness", Status: status, Message: msg, Observed: manifest, Details: map[string]interface{}{"source_size": info.Size(), "source_mod_time": info.ModTime().Format(time.RFC3339)}}
}

func marketStatusChecks(status interface{}) []diagnosticsCheck {
	root, _ := status.(map[string]interface{})
	result, _ := root["result"].(map[string]interface{})
	if result == nil {
		result = root
	}
	checks := []diagnosticsCheck{}
	if ready, _ := result["ready"].(bool); !ready {
		checks = append(checks, diagnosticsCheck{Name: "market ready", Status: diagWarn, Message: "market app did not report ready", Observed: result["ready"]})
	} else {
		checks = append(checks, diagnosticsCheck{Name: "market ready", Status: diagOK, Message: "market app ready"})
	}
	if item, _ := result["iteminfo"].(map[string]interface{}); item != nil {
		status := diagOK
		msg := "iteminfo status has no error"
		if errText, _ := item["error"].(string); errText != "" {
			status = diagError
			msg = errText
		}
		checks = append(checks, diagnosticsCheck{Name: "market iteminfo status", Status: status, Message: msg, Observed: item})
	}
	if services, _ := result["services"].(map[string]interface{}); services != nil {
		names := make([]string, 0, len(services))
		for name := range services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			svc, _ := services[name].(map[string]interface{})
			ready := svc["status"] == "ready" && svc["listening"] == true
			status := diagOK
			msg := "service ready"
			if !ready {
				status = diagError
				msg = "service is not ready"
			}
			checks = append(checks, diagnosticsCheck{Name: "market service " + name, Status: status, Message: msg, Observed: svc})
		}
	}
	if policies, _ := result["policy"].(map[string]interface{}); policies != nil {
		names := make([]string, 0, len(policies))
		for name := range policies {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			policy, _ := policies[name].(map[string]interface{})
			health, _ := policy["health"].(string)
			status := diagOK
			msg := "policy healthy"
			if health != "" && health != "ok" && health != "healthy" {
				status = diagWarn
				msg = "policy reports " + health
			}
			checks = append(checks, diagnosticsCheck{Name: "market policy " + name, Status: status, Message: msg, Observed: policy})
		}
	}
	return checks
}

func auctionGuardCheck(path string) diagnosticsCheck {
	data, err := os.ReadFile(path)
	if err != nil {
		return diagnosticsCheck{Name: "auction guard", Status: diagWarn, Message: err.Error(), Expected: path}
	}
	status := diagWarn
	msg := "auction guard is not installed"
	if bytes.Contains(data, []byte(diagnosticsAuctionGuardBegin)) && bytes.Contains(data, []byte(diagnosticsAuctionGuardEnd)) {
		status = diagOK
		msg = "auction guard is installed"
	}
	return diagnosticsCheck{Name: "auction guard", Status: status, Message: msg, Observed: map[string]interface{}{"path": path, "size": len(data)}}
}

func auctionMemoryPatchReadOnlyCheck() diagnosticsCheck {
	pid, err := pidOfProcess("df_auction_r")
	if err != nil {
		return diagnosticsCheck{Name: "auction memory patch", Status: diagWarn, Message: err.Error()}
	}
	const patched = byte(0x7f)
	addresses := map[string]int64{
		"refine_average_price_valid": 0x0806523f,
		"level_operate_parameter":    0x080811d7,
		"refine_search_valid":        0x0808281f,
		"level_specific":             0x0808292d,
		"level_category_min":         0x08083472,
		"level_category_max":         0x0808347d,
	}
	mem, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		return diagnosticsCheck{Name: "auction memory patch", Status: diagWarn, Message: err.Error(), Observed: map[string]interface{}{"pid": pid}}
	}
	defer mem.Close()
	ok := 0
	entries := map[string]interface{}{}
	for name, addr := range addresses {
		var buf [1]byte
		if _, err := mem.ReadAt(buf[:], addr); err != nil {
			entries[name] = err.Error()
			continue
		}
		entries[name] = fmt.Sprintf("0x%02x", buf[0])
		if buf[0] == patched {
			ok++
		}
	}
	status := diagOK
	msg := "auction memory patch appears active"
	if ok != len(addresses) {
		status = diagWarn
		msg = fmt.Sprintf("patched bytes %d/%d", ok, len(addresses))
	}
	return diagnosticsCheck{Name: "auction memory patch", Status: status, Message: msg, Observed: map[string]interface{}{"pid": pid, "entries": entries}}
}

func pidOfProcess(name string) (int, error) {
	out, err := exec.Command("pidof", name).Output()
	if err != nil {
		return 0, fmt.Errorf("%s pid not found: %w", name, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s pid not found", name)
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid %s pid %q", name, fields[0])
	}
	return pid, nil
}

func partyCompatDiagStatus(status partyCompatStatus) string {
	if status.DesiredEnabled && (!status.Enabled || status.State != "on") {
		if status.State == "unavailable" {
			return diagWarn
		}
		return diagError
	}
	if !status.DesiredEnabled && status.Enabled {
		return diagWarn
	}
	return diagOK
}

func partyCompatDiagMessage(status partyCompatStatus) string {
	if status.Message != "" {
		return status.Message
	}
	if status.DesiredEnabled && status.Enabled {
		return "party compatibility patch is active"
	}
	if status.DesiredEnabled {
		return "party compatibility patch is desired but not active"
	}
	if status.Enabled {
		return "party compatibility patch is active while desired off"
	}
	return "party compatibility patch is off"
}

func portDialCheck(name, host string, port int) diagnosticsCheck {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return diagnosticsCheck{Name: name, Status: diagError, Message: err.Error(), Expected: addr}
	}
	_ = conn.Close()
	return diagnosticsCheck{Name: name, Status: diagOK, Message: "tcp port is reachable", Observed: addr}
}

func udpListeningCheck(name string, port int) diagnosticsCheck {
	out, err := exec.Command("ss", "-lunp").Output()
	if err != nil {
		return diagnosticsCheck{Name: name, Status: diagWarn, Message: err.Error(), Expected: port}
	}
	pattern := regexp.MustCompile(`[:.]` + regexp.QuoteMeta(strconv.Itoa(port)) + `\s`)
	for _, line := range strings.Split(string(out), "\n") {
		if pattern.MatchString(line) {
			return diagnosticsCheck{Name: name, Status: diagOK, Message: "udp port is listening", Observed: strings.TrimSpace(line)}
		}
	}
	return diagnosticsCheck{Name: name, Status: diagError, Message: "udp port is not listening", Expected: port}
}

func skillDiagnosticsChecks(configDir string) []diagnosticsCheck {
	whitelistPath := filepath.Join(configDir, "party_skill_catalog.json")
	pvfPath := filepath.Join(configDir, "pvf_skill_state_catalog.json")

	whitelist, whitelistErr := catalog.ReadPartySkillCatalog(whitelistPath)
	whitelistCheck := partySkillWhitelistCheck(whitelistPath, whitelist, whitelistErr)
	pvfStates, pvfCheck, pvfErr := pvfSkillCatalogCheck(pvfPath)
	checks := []diagnosticsCheck{whitelistCheck, pvfCheck}
	if whitelistErr != nil || pvfErr != nil {
		checks = append(checks, diagnosticsCheck{
			Name: "effective party skill candidates", Status: diagError,
			Message: "candidate intersection cannot be calculated until both catalogs are valid",
		})
		return checks
	}
	checks = append(checks, effectivePartySkillCheck(whitelist, pvfStates))
	return checks
}

func partySkillWhitelistCheck(path string, report catalog.PartySkillCatalogReport, err error) diagnosticsCheck {
	if err != nil {
		return diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}
	}
	byJob := make(map[int]int)
	for _, entry := range report.Entries {
		byJob[entry.Job]++
	}
	status := diagOK
	message := "party skill whitelist is valid"
	switch {
	case len(report.Entries) == 0:
		status = diagWarn
		message = "party skill whitelist has no valid entries"
	case len(report.Issues) > 0 || report.OverLevelCount > 0 || report.ConfiguredMaxSkillLevel > report.EffectiveMaxSkillLevel:
		status = diagWarn
		message = "party skill whitelist contains skipped entries"
	}
	return diagnosticsCheck{
		Name: filepath.Base(path), Status: status, Message: message,
		Observed: map[string]interface{}{
			"path": path, "source_count": report.SourceCount, "valid_count": len(report.Entries), "by_job": byJob,
			"invalid_count": len(report.Issues), "disabled_count": report.DisabledCount, "over_level_count": report.OverLevelCount,
			"configured_max_level": report.ConfiguredMaxSkillLevel, "effective_max_level": report.EffectiveMaxSkillLevel,
		},
	}
}

func pvfSkillCatalogCheck(path string) ([]shared.SkillState, diagnosticsCheck, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}, err
	}
	var entries []shared.SkillState
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, diagnosticsCheck{Name: filepath.Base(path), Status: diagError, Message: err.Error(), Expected: path}, err
	}
	byJob := make(map[int]int)
	for _, entry := range entries {
		byJob[entry.Job]++
	}
	status := diagOK
	message := "PVF skill catalog is valid"
	if len(entries) == 0 {
		status = diagWarn
		message = "PVF skill catalog has no entries"
	}
	return entries, diagnosticsCheck{
		Name: filepath.Base(path), Status: status, Message: message,
		Observed: map[string]interface{}{"path": path, "count": len(entries), "by_job": byJob},
	}, nil
}

func effectivePartySkillCheck(whitelist catalog.PartySkillCatalogReport, pvfStates []shared.SkillState) diagnosticsCheck {
	jobSet := make(map[int]struct{})
	for _, entry := range whitelist.Entries {
		jobSet[entry.Job] = struct{}{}
	}
	jobs := make([]int, 0, len(jobSet))
	for job := range jobSet {
		jobs = append(jobs, job)
	}
	sort.Ints(jobs)

	byJob := make(map[int]int, len(jobs))
	total := 0
	stats := shared.PartySkillMatchStats{}
	for _, job := range jobs {
		matches, jobStats := shared.MatchPartySkillStates(job, whitelist.Entries, pvfStates)
		byJob[job] = len(matches)
		total += len(matches)
		stats.PVFMatched += jobStats.PVFMatched
		stats.SkippedMissingPVF += jobStats.SkippedMissingPVF
		stats.SkippedPathMismatch += jobStats.SkippedPathMismatch
	}

	status := diagOK
	message := fmt.Sprintf("%d effective party skill candidates", total)
	switch {
	case total == 0:
		status = diagError
		message = "party skill whitelist and PVF export have no effective candidates"
	case len(whitelist.Issues) > 0 || whitelist.OverLevelCount > 0 || stats.SkippedMissingPVF > 0 || stats.SkippedPathMismatch > 0:
		status = diagWarn
		message = fmt.Sprintf("%d effective party skill candidates with skipped entries", total)
	}
	return diagnosticsCheck{
		Name: "effective party skill candidates", Status: status, Message: message,
		Observed: map[string]interface{}{
			"total": total, "by_job": byJob, "whitelist_valid": len(whitelist.Entries), "whitelist_invalid": len(whitelist.Issues),
			"pvf_total": len(pvfStates), "missing_pvf": stats.SkippedMissingPVF, "path_mismatch": stats.SkippedPathMismatch,
		},
	}
}

func recentLogPatternCheck(name, path string, patterns []string) diagnosticsCheck {
	text, err := tailText(path, 1024*1024)
	if err != nil {
		return diagnosticsCheck{Name: name, Status: diagWarn, Message: err.Error(), Expected: path}
	}
	hits := map[string]int{}
	total := 0
	lower := strings.ToLower(text)
	for _, pattern := range patterns {
		count := strings.Count(lower, strings.ToLower(pattern))
		if count > 0 {
			hits[pattern] = count
			total += count
		}
	}
	if total > 0 {
		return diagnosticsCheck{Name: name, Status: diagWarn, Message: fmt.Sprintf("found %d recent keyword hits", total), Observed: hits}
	}
	return diagnosticsCheck{Name: name, Status: diagOK, Message: "no recent keyword hits", Observed: path}
}

func tailText(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	return buf.String(), scanner.Err()
}

func logSizeCheck(path string, limit int64) diagnosticsCheck {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return diagnosticsCheck{Name: filepath.Base(path), Status: diagOK, Message: "log file does not exist yet", Observed: path}
	}
	if err != nil {
		return diagnosticsCheck{Name: filepath.Base(path), Status: diagWarn, Message: err.Error(), Observed: path}
	}
	status := diagOK
	msg := "log size is within configured limit"
	if info.Size() > limit {
		status = diagWarn
		msg = "log file exceeds configured per-file limit"
	}
	return diagnosticsCheck{Name: filepath.Base(path), Status: status, Message: msg, Expected: limit, Observed: map[string]interface{}{"path": path, "size": info.Size(), "mod_time": info.ModTime().Format(time.RFC3339)}}
}

func boolCheck(name string, ok bool, err error, okMsg, badMsg string, observed interface{}) diagnosticsCheck {
	if err != nil {
		return diagnosticsCheck{Name: name, Status: diagError, Message: err.Error(), Observed: observed}
	}
	if !ok {
		return diagnosticsCheck{Name: name, Status: diagError, Message: badMsg, Observed: observed}
	}
	return diagnosticsCheck{Name: name, Status: diagOK, Message: okMsg, Observed: observed}
}
