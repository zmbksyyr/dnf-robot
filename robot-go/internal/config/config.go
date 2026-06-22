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
	RobotPort            int
	DBHost               string
	DBPort               int
	DBName               string
	DBUser               string
	DBPassword           string
	DFGameR              string
	ConfigDir            string
	RobotInnerIP         string
	RobotConnectIP       string
	RobotGamePort        int
	DBInitSize           int
	DBMaxSize            int
	DBConnectionTimeout  int
	WebPort              int
	WebPassword          string
	LogMaxSizeMB         int
	LogMaxBackups        int
	MaxResponseBytes     int
	ThisIP               string
	defaultConfigWritten bool
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

	// [Robot] section
	cfg.RobotPort = ini.GetInt("Robot", "robotPort", 8111)
	cfg.DFGameR = ini.GetString("Robot", "DfGameR", "/home/neople/game/df_game_r")
	cfg.ConfigDir = ini.GetString("Robot", "ConfigDir", "./config")
	cfg.RobotInnerIP = ini.GetString("Robot", "RobotInnerIp", "")
	if cfg.RobotInnerIP == "" {
		cfg.RobotInnerIP = "10.0.0.1"
	}
	cfg.RobotConnectIP = ini.GetString("Robot", "RobotConnectIp", "")
	cfg.RobotGamePort = ini.GetInt("Robot", "RobotGamePort", 10011)

	// [Web] section
	cfg.WebPort = ini.GetInt("Web", "WebPort", 8112)
	if cfg.WebPort <= 0 {
		cfg.WebPort = 8112
	}
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
		"# robot 主配置。修改后需要重启 robot。",
		"[Robot]",
		"# robot TCP API 端口，Web/CLI 通过它发送命令。",
		"robotPort = 8111",
		"# df_game_r path, used for runtime self-check and PVF export.",
		"DfGameR = /home/neople/game/df_game_r",
		"# 运行配置目录，robot_config.ini、模板、PVF 导出和日志都在这里。",
		"ConfigDir = ./config",
		"# 写入假人登录数据里的游戏内网 IP。",
		"RobotInnerIp = 10.0.0.1",
		"# robot 检测游戏端口、假人连接游戏端口使用的 IP；留空自动取本机 IP。",
		"RobotConnectIp = ",
		"# 假人连接的游戏频道端口。",
		"RobotGamePort = 10011",
		"",
		"[Web]",
		"# Web 管理页面端口。",
		"WebPort = 8112",
		"# Web 登录密码。",
		"WebPassword = twadmin",
		"",
		"[db]",
		"# MySQL 连接。robot 会自动准备假人所需数据库表。",
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
