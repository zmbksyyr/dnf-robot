package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

const launcherConfigName = "deploy-launcher.ini"

type launcherConfig struct {
	Host         string
	Port         string
	User         string
	Password     string
	FreshInstall bool
}

func defaultLauncherConfig() launcherConfig {
	return launcherConfig{
		Host:         "192.168.200.131",
		Port:         "22",
		User:         "root",
		Password:     "123456",
		FreshInstall: true,
	}
}

func loadLauncherConfig(path string) (launcherConfig, error) {
	config := defaultLauncherConfig()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if writeErr := os.WriteFile(path, formatLauncherConfig(config), 0600); writeErr != nil {
			return config, fmt.Errorf("create %s: %w", path, writeErr)
		}
		return config, nil
	}
	if err != nil {
		return config, fmt.Errorf("read %s: %w", path, err)
	}

	section := ""
	freshInstallSeen := false
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = strings.TrimSpace(line[1:end])
			}
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:separator]))
		value := strings.TrimSpace(line[separator+1:])
		switch {
		case strings.EqualFold(section, "ssh"):
			if value == "" {
				continue
			}
			switch key {
			case "host":
				config.Host = value
			case "port":
				config.Port = value
			case "user":
				config.User = value
			case "password":
				config.Password = value
			}
		case strings.EqualFold(section, "deploy") && key == "fresh_install":
			freshInstallSeen = true
			switch value {
			case "1":
				config.FreshInstall = true
			case "0":
				config.FreshInstall = false
			default:
				// A malformed value must never turn an intended in-place upgrade
				// into a destructive fresh deployment.
				config.FreshInstall = false
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return defaultLauncherConfig(), fmt.Errorf("parse %s: %w", path, err)
	}
	if !freshInstallSeen {
		separator := "\n"
		if len(raw) == 0 || raw[len(raw)-1] == '\n' {
			separator = ""
		}
		addition := separator + "\n" + formatDeployConfig(config.FreshInstall)
		file, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if openErr != nil {
			return config, fmt.Errorf("open %s for deploy config migration: %w", path, openErr)
		}
		_, writeErr := file.WriteString(addition)
		closeErr := file.Close()
		if writeErr != nil {
			return config, fmt.Errorf("append deploy settings to %s: %w", path, writeErr)
		}
		if closeErr != nil {
			return config, fmt.Errorf("close %s after deploy config migration: %w", path, closeErr)
		}
	}
	return config, nil
}

func formatLauncherConfig(config launcherConfig) []byte {
	return []byte(fmt.Sprintf(`[ssh]
host = %s
port = %s
user = %s
password = %s

%s`, config.Host, config.Port, config.User, config.Password, formatDeployConfig(config.FreshInstall)))
}

func formatDeployConfig(freshInstall bool) string {
	value := 0
	if freshInstall {
		value = 1
	}
	return fmt.Sprintf(`[deploy]
# 1 = 全新安装：备份并重建 /root/config，再部署 robot
# 0 = 保留配置升级：只替换 /root/robot，保留 /root/config
# “重启 robot”始终保留 /root/config，不受此配置影响
fresh_install = %d
`, value)
}
