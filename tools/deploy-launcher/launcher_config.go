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
			// Invalid explicit values preserve the remote configuration. This is
			// safer than turning a typo into a destructive fresh deployment.
			config.FreshInstall = value == "1"
		}
	}
	if err := scanner.Err(); err != nil {
		return defaultLauncherConfig(), fmt.Errorf("parse %s: %w", path, err)
	}
	return config, nil
}

func formatLauncherConfig(config launcherConfig) []byte {
	freshInstall := 0
	if config.FreshInstall {
		freshInstall = 1
	}
	return []byte(fmt.Sprintf("[ssh]\nhost = %s\nport = %s\nuser = %s\npassword = %s\n\n[deploy]\n# 1 = fresh config, 0 = keep existing config\nfresh_install = %d\n", config.Host, config.Port, config.User, config.Password, freshInstall))
}
