package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	remoteConfigPath    = "/root/config/config.ini"
	defaultRobotAPIPort = 8111
	defaultWebPort      = 8112
)

type robotListenPorts struct {
	robotAPI int
	web      int
}

func readRemoteRobotListenPorts(client *ssh.Client) (robotListenPorts, error) {
	return loadRobotListenPorts(func() (string, error) {
		return runCmdOutput(client, "cat -- "+remoteConfigPath)
	})
}

func loadRobotListenPorts(readConfig func() (string, error)) (robotListenPorts, error) {
	raw, err := readConfig()
	if err != nil {
		return robotListenPorts{}, fmt.Errorf("读取远端配置 %s 失败: %w", remoteConfigPath, err)
	}
	ports, err := parseRobotListenPorts(raw)
	if err != nil {
		return robotListenPorts{}, fmt.Errorf("解析远端配置 %s 失败: %w", remoteConfigPath, err)
	}
	return ports, nil
}

func parseRobotListenPorts(raw string) (robotListenPorts, error) {
	ports := robotListenPorts{robotAPI: defaultRobotAPIPort, web: defaultWebPort}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(raw))
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
		if section != "Ports" {
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		switch key {
		case "RobotAPI":
			ports.robotAPI = validConfiguredPort(value, defaultRobotAPIPort)
		case "Web":
			ports.web = validConfiguredPort(value, defaultWebPort)
		}
	}
	if err := scanner.Err(); err != nil {
		return robotListenPorts{}, err
	}
	return ports, nil
}

func validConfiguredPort(raw string, fallback int) int {
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > 65535 {
		return fallback
	}
	return port
}

func listenerPortsReady(raw string, expected robotListenPorts) bool {
	foundAPI := false
	foundWeb := false
	for _, address := range strings.Fields(raw) {
		separator := strings.LastIndexByte(address, ':')
		if separator < 0 {
			continue
		}
		port, err := strconv.Atoi(address[separator+1:])
		if err != nil {
			continue
		}
		foundAPI = foundAPI || port == expected.robotAPI
		foundWeb = foundWeb || port == expected.web
	}
	return foundAPI && foundWeb
}
