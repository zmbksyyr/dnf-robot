package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	remoteConfigPath    = "/root/config/conf/config.ini"
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
		return robotListenPorts{}, fmt.Errorf("读取远程配置 %s 失败: %w", remoteConfigPath, err)
	}
	ports, err := parseRobotListenPorts(raw)
	if err != nil {
		return robotListenPorts{}, fmt.Errorf("解析远程配置 %s 失败: %w", remoteConfigPath, err)
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
			if !strings.HasSuffix(line, "]") || len(line) < 3 {
				return robotListenPorts{}, fmt.Errorf("无效 section %q", line)
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "Ports" {
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 0 {
			return robotListenPorts{}, fmt.Errorf("Ports 配置行缺少等号: %q", line)
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		switch key {
		case "RobotAPI":
			port, err := validConfiguredPort(value)
			if err != nil {
				return robotListenPorts{}, fmt.Errorf("RobotAPI: %w", err)
			}
			ports.robotAPI = port
		case "Web":
			port, err := validConfiguredPort(value)
			if err != nil {
				return robotListenPorts{}, fmt.Errorf("Web: %w", err)
			}
			ports.web = port
		}
	}
	if err := scanner.Err(); err != nil {
		return robotListenPorts{}, err
	}
	return ports, nil
}

func validConfiguredPort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("端口必须是整数")
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("端口必须在 1-65535 之间")
	}
	return port, nil
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
