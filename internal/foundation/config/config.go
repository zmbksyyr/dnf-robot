package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const configFile = "./config/config.ini"

// SysConfig holds all robot configuration from config.ini.
type SysConfig struct {
	RobotPort           int
	DBHost              string
	DBPort              int
	DBName              string
	DBUser              string
	DBPassword          string
	DFGameR             string
	ConfigDir           string
	RobotInnerIP        string
	RobotConnectIP      string
	RobotGamePort       int
	MonitorPort         int
	AuctionPort         int
	PointPort           int
	RelayPort           int
	PartyRoute0Port     int
	DBInitSize          int
	DBMaxSize           int
	DBConnectionTimeout int
	WebPort             int
	WebPassword         string
	LogMaxSizeMB        int
	LogMaxBackups       int
	MaxResponseBytes    int
	ThisIP              string
}

// LoadConfig reads config.ini and returns a populated SysConfig.
// If the config file does not exist, a default one is generated first.
func LoadConfig(path string) (*SysConfig, error) {
	if path == "" {
		path = configFile
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := generateDefaultConfig(path); err != nil {
			return nil, fmt.Errorf("generate default config: %w", err)
		}
	}

	ini, err := Load(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &SysConfig{}

	// [Ports] section
	cfg.RobotPort = validPort(ini.GetInt("Ports", "RobotAPI", 8111), 8111)
	cfg.WebPort = validPort(ini.GetInt("Ports", "Web", 8112), 8112)
	cfg.RobotGamePort = validPort(ini.GetInt("Ports", "Game", 10011), 10011)
	cfg.MonitorPort = validPort(ini.GetInt("Ports", "Monitor", 30303), 30303)
	cfg.AuctionPort = validPort(ini.GetInt("Ports", "Auction", 30803), 30803)
	cfg.PointPort = validPort(ini.GetInt("Ports", "Point", 30603), 30603)
	cfg.RelayPort = validPort(ini.GetInt("Ports", "Relay", 7200), 7200)
	cfg.PartyRoute0Port = validPort(ini.GetInt("Ports", "PartyRoute0", 5063), 5063)

	// [Robot] section
	cfg.DFGameR = ini.GetString("Robot", "DfGameR", "/home/neople/game/df_game_r")
	cfg.ConfigDir = ini.GetString("Robot", "ConfigDir", "./config")
	cfg.RobotInnerIP = ini.GetString("Robot", "RobotInnerIp", "")
	if cfg.RobotInnerIP == "" {
		cfg.RobotInnerIP = "10.0.0.1"
	}
	cfg.RobotConnectIP = ini.GetString("Robot", "RobotConnectIp", "")

	// [Web] section
	cfg.WebPassword = ini.GetString("Web", "WebPassword", "twadmin")

	// [db] section
	cfg.DBHost = ini.GetString("db", "db_host", "127.0.0.1")
	cfg.DBUser = ini.GetString("db", "db_user_name", "game")
	cfg.DBPassword = ini.GetString("db", "db_password", "uu5!^%jg")
	cfg.DBName = ini.GetString("db", "db_database_name", "d_taiwan")
	cfg.DBPort = ini.GetInt("db", "db_prot", 3306)
	cfg.DBInitSize = ini.GetInt("db", "db_init_size", 4)
	if cfg.DBInitSize <= 0 {
		cfg.DBInitSize = 4
	}
	cfg.DBMaxSize = ini.GetInt("db", "db_max_Size", 64)
	if cfg.DBMaxSize <= 0 {
		cfg.DBMaxSize = 64
	}
	cfg.DBConnectionTimeout = ini.GetInt("db", "db_connection_time_out", 300)
	if cfg.DBConnectionTimeout <= 0 {
		cfg.DBConnectionTimeout = 300
	}
	if cfg.DBMaxSize < cfg.DBInitSize {
		cfg.DBMaxSize = cfg.DBInitSize
	}

	// [system] section
	cfg.LogMaxSizeMB = ini.GetInt("system", "log_max_size_mb", 100)
	if cfg.LogMaxSizeMB <= 0 {
		cfg.LogMaxSizeMB = 100
	}
	cfg.LogMaxBackups = ini.GetInt("system", "log_max_backups", 5)
	if cfg.LogMaxBackups <= 0 {
		cfg.LogMaxBackups = 5
	}
	cfg.MaxResponseBytes = ini.GetInt("system", "max_response_bytes", 4*1024*1024)
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 4 * 1024 * 1024
	}

	// auto-detect local IP
	cfg.ThisIP = getLocalIP()
	if cfg.RobotConnectIP == "" {
		cfg.RobotConnectIP = cfg.ThisIP
	}

	return cfg, nil
}

func validPort(port, fallback int) int {
	if port <= 0 || port > 65535 {
		return fallback
	}
	return port
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

func generateDefaultConfig(path string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(strings.Join([]string{
		"# robot main config. Restart robot after editing.",
		"[Ports]",
		"RobotAPI = 8111",
		"Web = 8112",
		"Game = 10011",
		"Monitor = 30303",
		"Auction = 30803",
		"Point = 30603",
		"Relay = 7200",
		"PartyRoute0 = 5063",
		"",
		"[Robot]",
		"# df_game_r path, used for runtime self-check and PVF export.",
		"DfGameR = /home/neople/game/df_game_r",
		"# Runtime config directory for robot_config.ini, templates, PVF exports, and logs.",
		"ConfigDir = ./config",
		"# Inner game IP written into robot login data.",
		"RobotInnerIp = 10.0.0.1",
		"# IP used for game-port checks and robot game connection; leave empty to auto-detect local IP.",
		"RobotConnectIp = ",
		"",
		"[Web]",
		"# Web login password.",
		"WebPassword = twadmin",
		"",
		"[db]",
		"# MySQL connection. Robot prepares required robot tables automatically.",
		"db_host = 127.0.0.1",
		"db_user_name = game",
		"db_password = uu5!^%jg",
		"db_database_name = d_taiwan",
		"db_prot = 3306",
		"db_init_size = 4",
		"db_max_Size = 64",
		"db_connection_time_out = 300",
		"",
		"[system]",
		"log_max_size_mb = 100",
		"log_max_backups = 5",
		"max_response_bytes = 4194304",
		"",
	}, "\n"))
	return err
}
