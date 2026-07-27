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
	Host     string
	User     string
	Password string
}

func defaultLauncherConfig() launcherConfig {
	return launcherConfig{
		Host:     "192.168.200.131",
		User:     "root",
		Password: "123456",
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
		if !strings.EqualFold(section, "ssh") {
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:separator]))
		value := strings.TrimSpace(line[separator+1:])
		if value == "" {
			continue
		}
		switch key {
		case "host":
			config.Host = value
		case "user":
			config.User = value
		case "password":
			config.Password = value
		}
	}
	if err := scanner.Err(); err != nil {
		return defaultLauncherConfig(), fmt.Errorf("parse %s: %w", path, err)
	}
	return config, nil
}

func formatLauncherConfig(config launcherConfig) []byte {
	return []byte(fmt.Sprintf("[ssh]\nhost = %s\nuser = %s\npassword = %s\n", config.Host, config.User, config.Password))
}
