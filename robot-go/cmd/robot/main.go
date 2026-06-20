package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"robot/internal/config"
	"robot/internal/dnf"
	"robot/internal/network"
	"robot/internal/service"
	"robot/internal/webadmin"
)

var db *sql.DB
var asyncActions sync.Map

func main() {
	webAdminMode := flag.Bool("web-admin", false, "run web admin child process")
	robotAddr := flag.String("robot-addr", "", "robot TCP address for web admin")
	webAddr := flag.String("web-addr", "", "web admin listen address")
	flag.Parse()

	if *webAdminMode {
		runWebAdmin(*robotAddr, *webAddr)
		return
	}

	dnf.PrintfGreen("robot starting...\n")

	configPath, configDir, err := runtimeConfigPaths()
	if err != nil {
		fmt.Printf("resolve config path error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("load config error: %v\n", err)
		os.Exit(1)
	}
	cfg.ConfigDir = configDir
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		fmt.Printf("create config dir error: %v\n", err)
		os.Exit(1)
	}

	dnf.ConfigureLogRotation(cfg.LogMaxSizeMB, cfg.LogMaxBackups)
	if err := dnf.LogInit(filepath.Join(cfg.ConfigDir, "log_robot")); err != nil {
		fmt.Printf("init log error: %v\n", err)
		os.Exit(1)
	}
	defer dnf.LogClose()
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("ROBOT_CONFIG path=%s config_dir=%s\n", configPath, cfg.ConfigDir))

	if err := service.InitRuntime(cfg); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("ROBOT_RUNTIME_INIT_FAILED err=%v\n", err))
		dnf.PrintfRed("runtime init failed: %v\n", err)
		os.Exit(1)
	}

	initRSA(cfg)

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8&timeout=%ds&readTimeout=%ds&writeTimeout=%ds&parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName,
		cfg.DBConnectionTimeout, cfg.DBConnectionTimeout, cfg.DBConnectionTimeout)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		dnf.PrintfRed("database open failed: %v\n", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(cfg.DBMaxSize)
	db.SetMaxIdleConns(cfg.DBInitSize)
	db.SetConnMaxLifetime(time.Duration(cfg.DBConnectionTimeout) * time.Second)
	if err := db.Ping(); err != nil {
		dnf.PrintfRed("database ping failed: %v\n", err)
		os.Exit(1)
	}
	dnf.SetDBPool(db)

	robotSvc := service.GetRobotService()
	robotSvc.Init()
	service.SetRobotService(robotSvc)
	defer robotSvc.Shutdown()
	dollSvc := service.NewDollService()
	manager := service.NewRobotManager(db, cfg, dollSvc)
	manager.StartAutoActions()
	defer manager.StopAutoActions()
	stopWebAdmin := startWebAdminSupervisor(cfg)
	defer stopWebAdmin()

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.RobotPort)
	tcpServer := network.NewTCPServer(addr)
	tcpServer.SetLimits(256, 90*time.Second, 15*time.Second)
	tcpServer.OnMessage(func(clientID string, raw []byte) {
		response := handlePacket(clientID, string(raw), dollSvc, manager)
		if response != "" {
			_ = tcpServer.SendTo(clientID, []byte(response))
		}
	})
	go func() {
		if err := tcpServer.Start(); err != nil {
			dnf.PrintfRed("TCP server failed: %v\n", err)
		}
	}()
	logRobotActionf("TCP server listening on %s\n", addr)
	logRobotActionf("robot started\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logRobotActionf("robot stopping...\n")
	if err := tcpServer.Close(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("TCP_SERVER_CLOSE_FAILED err=%v\n", err))
		dnf.PrintfRed("tcp server close error: %v\n", err)
	}
	if err := db.Close(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("DATABASE_CLOSE_FAILED err=%v\n", err))
		dnf.PrintfRed("database close error: %v\n", err)
	}
	service.ClosePrivateKey()
}

func runWebAdmin(robotAddr, webAddr string) {
	configPath, configDir, err := runtimeConfigPaths()
	if err != nil {
		fmt.Printf("web admin resolve config path error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("web admin load config error: %v\n", err)
		os.Exit(1)
	}
	cfg.ConfigDir = configDir
	if robotAddr == "" {
		robotAddr = fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	}
	if webAddr == "" {
		webAddr = fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	}
	if err := webadmin.New(cfg, robotAddr, webAddr).ListenAndServe(); err != nil {
		fmt.Printf("web admin failed: %v\n", err)
		os.Exit(1)
	}
}

func runtimeConfigPaths() (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	configDir := filepath.Join(filepath.Dir(exe), "config")
	return filepath.Join(configDir, "config.ini"), configDir, nil
}

func startWebAdminSupervisor(cfg *config.SysConfig) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			cmd := newWebAdminCommand(cfg)
			if cmd == nil {
				return
			}
			if err := cmd.Start(); err != nil {
				dnf.PrintfRed("web admin start failed: %v\n", err)
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
			dnf.PrintfBlue("web admin listening on %s pid=%d\n", fmt.Sprintf("0.0.0.0:%d", cfg.WebPort), pid)
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
				dnf.PrintfRed("web admin exited pid=%d err=%v; restarting\n", pid, err)
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

func newWebAdminCommand(cfg *config.SysConfig) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		dnf.PrintfRed("web admin executable lookup failed: %v\n", err)
		return nil
	}
	robotAddr := fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	webAddr := fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	cmd := exec.Command(exe, "--web-admin", "--robot-addr", robotAddr, "--web-addr", webAddr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func initRSA(cfg *config.SysConfig) {
	st := service.BuildKeypairStatus(cfg)
	if !st.GameValid {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOAD_BLOCKED state=%s reason=%s err=%s\n", st.KeyState, st.KeyReason, st.Error))
		dnf.PrintfBlue("WARNING: game RSA key is not valid. Robot business commands are blocked until a valid key is configured or default key is released.\n")
		return
	}
	path := filepath.Join(cfg.ConfigDir, "privatekey.pem")
	if err := service.InitPrivateKey(path); err == nil {
		dnf.SetRSAKey(service.GetRSAKey())
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOADED source=%s state=%s fingerprint=%s\n", path, st.KeyState, st.Fingerprint))
		dnf.PrintfGreen("loaded RSA private key from %s\n", path)
		return
	}
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOAD_FAILED source=%s\n", path))
	dnf.PrintfBlue("WARNING: privatekey.pem not found in config directory. Robot login tokens cannot be generated - ALL robots will fail authentication.\n")
}

func handlePacket(clientID, pkt string, dollSvc *service.DollService, manager *service.RobotManager) string {
	defer func() {
		if r := recover(); r != nil {
			logRobotActionf("[handlePacket] panic recovered client=%s err=%v\n", clientID, r)
		}
	}()
	cmd := extractTagContent(pkt, "c")
	if err := requireValidKeypair(cmd, manager); err != nil {
		return wrapResult(map[string]interface{}{"ok": false, "error": err.Error(), "result": manager.KeypairStatus()})
	}
	switch cmd {
	case "05":
		return ""
	case "sys":
		return wrapResult(map[string]interface{}{"ok": true, "message": "sys ok"})
	case "createRobots":
		var req service.RobotCreateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		robots, err := manager.CreateRobots(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "robots": robots})
	case "robotsOnline":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.OnlineManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsOnlineAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsOnlineAsync", requestScope(req), func() (string, error) {
			res, err := manager.OnlineManaged(req)
			logRobotCommandResult("robotsOnlineAsync", res, err)
			return service.CommandOperationSummary(res, err), err
		})
	case "robotsMove":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.MoveManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShout":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutBothManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShoutWorld":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutManaged(req, true)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShoutLocal":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutManaged(req, false)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsStore":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.StoreManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsStoreAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsStoreAsync", requestScope(req), func() (string, error) {
			res, err := manager.StoreManaged(req)
			logRobotCommandResult("robotsStoreAsync", res, err)
			return service.CommandOperationSummary(res, err), err
		})
	case "robotsStatus":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.RobotsStatus(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "robotsLogout":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.LogoutManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsLogoutAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsLogoutAsync", requestScope(req), func() (string, error) {
			res, err := manager.LogoutManaged(req)
			logRobotCommandResult("robotsLogoutAsync", res, err)
			return service.CommandOperationSummary(res, err), err
		})
	case "autoStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.AutoStatus()})
	case "schedulerStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SchedulerStatus()})
	case "operationStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.OperationStatus()})
	case "systemStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SystemStatus()})
	case "databaseStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.DatabaseStatus()})
	case "keypairStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.KeypairStatus()})
	case "keypairReleaseDefault":
		res, err := manager.ReleaseDefaultKeypair()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "autoStart":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SetAutoEnabled(true)})
	case "autoStop":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SetAutoEnabled(false)})
	case "robotConfigGet":
		res, err := manager.RobotConfig()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "robotConfigUpdate":
		var req service.RobotConfigUpdateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.UpdateRobotConfig(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "cleanupRobots":
		var req service.RobotCleanupRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.CleanupRobots(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "cleanupRobotsAsync":
		var req service.RobotCleanupRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueExclusiveAction("cleanupRobotsAsync", func() {
			res, err := manager.CleanupRobots(req)
			if err != nil {
				logRobotActionf("[WebAction] cleanupRobotsAsync failed err=%v\n", err)
				return
			}
			logRobotActionf("[WebAction] cleanupRobotsAsync done candidates=%d deleted=%d skipped=%d\n",
				len(res.Candidates), res.Deleted, res.Skipped)
		})
	default:
		dnf.PrintfBlue("unknown command: %s\n", cmd)
		return wrapResult(map[string]interface{}{"ok": false, "error": "unknown command"})
	}
}

func requireValidKeypair(cmd string, manager *service.RobotManager) error {
	if !requiresValidKeypair(cmd) {
		return nil
	}
	st := manager.KeypairStatus()
	if st.GameValid {
		return nil
	}
	if st.Error != "" {
		return fmt.Errorf("RSA key unavailable: %s", st.Error)
	}
	if st.KeyReason != "" {
		return fmt.Errorf("RSA key unavailable: %s", st.KeyReason)
	}
	return fmt.Errorf("RSA key unavailable")
}

func requiresValidKeypair(cmd string) bool {
	switch cmd {
	case "createRobots",
		"robotsOnline",
		"robotsOnlineAsync",
		"robotsMove",
		"robotsShout",
		"robotsShoutWorld",
		"robotsShoutLocal",
		"robotsStore",
		"robotsStoreAsync",
		"robotsLogout",
		"robotsLogoutAsync",
		"autoStart":
		return true
	default:
		return false
	}
}

func queueRobotAction(manager *service.RobotManager, name, scope string, fn func() (string, error)) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	op := manager.BeginOperation(name, scope)
	go func() {
		defer asyncActions.Delete(name)
		summary, err := fn()
		manager.CompleteOperation(op.ID, summary, err)
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued", "operation": op}})
}

func queueExclusiveAction(name string, fn func()) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	go func() {
		defer asyncActions.Delete(name)
		fn()
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued"}})
}

func logRobotCommandResult(name string, res service.RobotCommandResult, err error) {
	if err != nil {
		logRobotActionf("[WebAction] %s failed err=%v\n", name, err)
		return
	}
	logRobotActionf("[WebAction] %s done requested=%d accepted=%d confirmed=%d failed=%d\n",
		name, res.Requested, res.Accepted, res.Confirmed, res.Failed)
}

func logRobotActionf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	dnf.LogString(dnf.LogLevelIndispensable, msg)
}

func requestScope(req service.RobotCommandRequest) string {
	if len(req.UIDs) > 0 {
		return fmt.Sprintf("uids=%d", len(req.UIDs))
	}
	return fmt.Sprintf("count=%d", req.Count)
}

func parseRobotCommand(pkt string) (service.RobotCommandRequest, error) {
	var req service.RobotCommandRequest
	if err := decodePayload(pkt, &req); err != nil {
		return req, err
	}
	return req, nil
}

func decodePayload(pkt string, dst interface{}) error {
	payload := strings.TrimSpace(extractPayload(pkt))
	if payload == "" {
		payload = "{}"
	}
	if err := json.Unmarshal([]byte(payload), dst); err != nil {
		return fmt.Errorf("invalid json payload: %w", err)
	}
	return nil
}

func extractPayload(pkt string) string {
	if v := extractTagContent(pkt, "json"); v != "" {
		return v
	}
	if v := extractTagContent(pkt, "key"); v != "" {
		return v
	}
	return "{}"
}

func wrapResult(v interface{}) string {
	data, _ := json.Marshal(v)
	return "<tw><result>" + string(data) + "</result></tw>"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func extractTagContent(pkt, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(pkt, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(pkt[start:], closeTag)
	if end < 0 {
		return ""
	}
	return pkt[start : start+end]
}
