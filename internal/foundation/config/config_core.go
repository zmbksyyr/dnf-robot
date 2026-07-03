package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---- config.go ----
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
		"# robot main config. Restart robot after editing.",
		"[Robot]",
		"# robot TCP API port for Web/CLI commands.",
		"robotPort = 8111",
		"# df_game_r path, used for runtime self-check and PVF export.",
		"DfGameR = /home/neople/game/df_game_r",
		"# Runtime config directory for robot_config.ini, templates, PVF exports, and logs.",
		"ConfigDir = ./config",
		"# Inner game IP written into robot login data.",
		"RobotInnerIp = 10.0.0.1",
		"# IP used for game-port checks and robot game connection; leave empty to auto-detect local IP.",
		"RobotConnectIp = ",
		"# Game channel port used by robot connections.",
		"RobotGamePort = 10011",
		"",
		"[Web]",
		"# Web admin page port.",
		"WebPort = 8112",
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

// ---- ini.go ----
const (
	maxValue = 1024
)

// INIConfig represents a parsed INI configuration file.
type INIConfig struct {
	comment   byte
	separator byte
	data      map[string]map[string]string // section -> key -> value
}

// Load reads and parses an INI file. Comment char defaults to '#' and separator to '='.
// If filename is empty, an empty config is returned.
func Load(filename string) (*INIConfig, error) {
	cfg := &INIConfig{
		comment:   '#',
		separator: '=',
		data:      make(map[string]map[string]string),
	}

	if filename == "" {
		return cfg, nil
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return parseINI(f)
}

// LoadFromString parses INI content from a string.
func LoadFromString(content string) (*INIConfig, error) {
	return parseINI(strings.NewReader(content))
}

func parseINI(r interface {
	Read([]byte) (int, error)
}) (*INIConfig, error) {
	cfg := &INIConfig{
		comment:   '#',
		separator: '=',
		data:      make(map[string]map[string]string),
	}

	var section string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		raw := scanner.Text()

		raw = strings.TrimSpace(raw)
		if raw == "" || raw[0] == '\r' || raw[0] == '\n' || raw[0] == cfg.comment || raw[0] == ';' {
			continue
		}

		if raw[0] == '[' {
			if end := strings.IndexByte(raw, ']'); end > 0 {
				section = strings.TrimSpace(raw[1:end])
			}
			continue
		}

		if idx := strings.IndexByte(raw, cfg.separator); idx >= 0 {
			key := strings.TrimSpace(raw[:idx])
			value := strings.TrimSpace(raw[idx+1:])
			if section != "" && key != "" {
				if cfg.data[section] == nil {
					cfg.data[section] = make(map[string]string)
				}
				cfg.data[section][key] = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// GetString returns the value for the given section and key, or defaultVal if not found.
func (c *INIConfig) GetString(section, key, defaultVal string) string {
	if c == nil || c.data == nil {
		return defaultVal
	}
	if m, ok := c.data[section]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return defaultVal
}

// GetInt returns the integer value for the given section and key, or defaultVal if not found.
func (c *INIConfig) GetInt(section, key string, defaultVal int) int {
	if c == nil || c.data == nil {
		return defaultVal
	}
	if m, ok := c.data[section]; ok {
		if v, ok := m[key]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return defaultVal
}
