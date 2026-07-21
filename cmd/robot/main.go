package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	runtimeinit "robot/internal/bootstrap/runtime"
	"robot/internal/capability/keypair"
	"robot/internal/capability/marketapp"
	"robot/internal/capability/robotconfig"
	"robot/internal/composition/auctionapp"
	"robot/internal/entry/tcpapi"
	"robot/internal/entry/webadmin"
	"robot/internal/foundation/config"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/foundation/network"
	"robot/internal/foundation/process"
	"robot/internal/protocol/dnf"
	"robot/internal/protocol/dnfruntime"
	"robot/internal/protocol/monitor"
	"robot/internal/scheduler"
	schedulerrepo "robot/internal/scheduler/repository"
)

var db *sql.DB
var marketApp *marketapp.App

func main() {
	webAdminMode := flag.Bool("web-admin", false, "run web admin child process")
	robotAddr := flag.String("robot-addr", "", "robot TCP address for web admin")
	webAddr := flag.String("web-addr", "", "web admin listen address")
	flag.Parse()
	if boundedLogSinkRequested() {
		if err := runBoundedLogSink(os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "bounded log sink failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

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
	foundationlog.SetRobotSink(func(msg string) {
		dnf.LogString(dnf.LogLevelIndispensable, msg)
	})
	defer dnf.LogClose()
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("ROBOT_CONFIG path=%s config_dir=%s\n", configPath, cfg.ConfigDir))

	if err := runtimeinit.Init(cfg); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("ROBOT_RUNTIME_INIT_FAILED err=%v\n", err))
		dnf.PrintfRed("runtime init failed: %v\n", err)
		os.Exit(1)
	}
	robotRuntimeConfig, err := robotconfig.LoadFile(filepath.Join(cfg.ConfigDir, "robot_config.ini"))
	if err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("PARTY_ACCOUNT_RANGE_DEFAULTED err=%v\n", err))
		robotRuntimeConfig = robotconfig.Default()
	}
	if err := process.EnsureOpenFileLimit(robotRuntimeConfig.MaxOnlineRobots, cfg.DBMaxSize); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("OPEN_FILE_CAPACITY_FAILED err=%v\n", err))
		dnf.PrintfRed("open file capacity check failed: %v\n", err)
		os.Exit(1)
	}
	dnf.ConfigurePartyRelayPort(cfg.RelayPort)
	route0Sink, err := dnf.StartPartyRoute0Sink(cfg.PartyRoute0Port)
	if err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("PARTY_ROUTE0_SINK_FAILED addr=0.0.0.0:%d err=%v\n", cfg.PartyRoute0Port, err))
		dnf.PrintfRed("party route0 sink failed: %v\n", err)
		os.Exit(1)
	}
	defer route0Sink.Close()
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("PARTY_ROUTE0_SINK_READY addr=0.0.0.0:%d\n", cfg.PartyRoute0Port))
	dnf.ConfigurePartyRobotAccountRange(robotRuntimeConfig.RobotUIDStart, robotRuntimeConfig.RobotUIDEnd)
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("PARTY_ACCOUNT_RANGE start=%d end=%d\n", robotRuntimeConfig.RobotUIDStart, robotRuntimeConfig.RobotUIDEnd))
	keypair.SetRuntimeKeySink(dnf.SetRSAKey)

	initRSA(cfg)

	db, err = openDatabase(cfg)
	if err != nil {
		dnf.PrintfRed("database open failed: %v\n", err)
		os.Exit(1)
	}
	dnf.SetDBPool(db)

	robotSvc := dnfruntime.NewRobotService()
	manager := scheduler.NewRobotManager(schedulerrepo.NewSQLRepository(db), cfg, robotSvc)
	manager.SetWorldShout(&monitor.Client{Address: fmt.Sprintf("127.0.0.1:%d", cfg.MonitorPort)})
	marketApp, err = marketapp.New(db, cfg, auctionapp.NewFactory())
	if err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("MARKET_INIT_FAILED err=%v\n", err))
		dnf.PrintfRed("market init failed: %v\n", err)
		os.Exit(1)
	}
	tcpapi.SetMarketApp(marketApp)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.RobotPort)
	tcpServer := network.NewTCPServer(addr)
	tcpServer.SetLimits(256, 90*time.Second, 15*time.Second)
	tcpServer.OnMessage(func(clientID string, raw []byte) {
		response := tcpapi.HandlePacket(clientID, string(raw), manager)
		if response != "" {
			_ = tcpServer.SendTo(clientID, []byte(response))
		}
	})
	if err := tcpServer.Start(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("TCP_SERVER_START_FAILED addr=%s err=%v\n", addr, err))
		dnf.PrintfRed("TCP server failed: %v\n", err)
		os.Exit(1)
	}
	logRobotActionf("TCP server listening on %s\n", addr)
	stopWebAdmin := webadmin.StartSupervisor(cfg)
	manager.StartAutoActions()
	if marketApp.Config().Auto.Enabled {
		marketApp.StartAuto()
	}
	logRobotActionf("robot started\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logRobotActionf("robot stopping...\n")
	if err := tcpServer.Close(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("TCP_SERVER_CLOSE_FAILED err=%v\n", err))
		dnf.PrintfRed("tcp server close error: %v\n", err)
	}
	stopWebAdmin()
	marketApp.Shutdown()
	if err := manager.Shutdown(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("ROBOT_MANAGER_SHUTDOWN_FAILED err=%v\n", err))
		dnf.PrintfRed("robot manager shutdown error: %v\n", err)
	}
	robotSvc.Shutdown()
	if err := db.Close(); err != nil {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("DATABASE_CLOSE_FAILED err=%v\n", err))
		dnf.PrintfRed("database close error: %v\n", err)
	}
	keypair.ClosePrivateKey()
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

func initRSA(cfg *config.SysConfig) {
	st := keypair.BuildKeypairStatus(cfg)
	if !st.GameValid {
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOAD_BLOCKED state=%s reason=%s err=%s\n", st.KeyState, st.KeyReason, st.Error))
		dnf.PrintfBlue("WARNING: game RSA key is not valid. Robot business commands are blocked until a valid key is configured or default key is released.\n")
		return
	}
	path := filepath.Join(cfg.ConfigDir, "privatekey.pem")
	if err := keypair.InitPrivateKey(path); err == nil {
		dnf.SetRSAKey(keypair.GetRSAKey())
		dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOADED source=%s state=%s fingerprint=%s\n", path, st.KeyState, st.Fingerprint))
		dnf.PrintfGreen("loaded RSA private key from %s\n", path)
		return
	}
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("KEYPAIR_RSA_LOAD_FAILED source=%s\n", path))
	dnf.PrintfBlue("WARNING: privatekey.pem not found in config directory. Robot login tokens cannot be generated - ALL robots will fail authentication.\n")
}

func logRobotActionf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}
