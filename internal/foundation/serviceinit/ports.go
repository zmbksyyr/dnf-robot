package serviceinit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const defaultServiceRoot = "/home/neople"

type PortValue struct {
	Port   int
	Source string
}

type ExternalPorts struct {
	Game    PortValue
	Monitor PortValue
	Auction PortValue
	Point   PortValue
	Relay   PortValue
}

// DiscoverExternalPorts follows service commands from /root/run to their
// referenced config files. Missing or ambiguous values are left at zero so
// the caller can apply its own established defaults.
func DiscoverExternalPorts(runScript, homeRoot string) ExternalPorts {
	root := defaultServiceRoot
	gameLaunch, gameErr := DiscoverLaunch("game", root, runScript, homeRoot)
	if gameErr == nil && filepath.Base(gameLaunch.Dir) == "game" {
		root = filepath.Dir(gameLaunch.Dir)
	}

	discover := func(name string, existing Launch, existingErr error) PortValue {
		launch, err := existing, existingErr
		if launch.Bin == "" {
			launch, err = DiscoverLaunch(name, root, runScript, homeRoot)
		}
		if err != nil {
			return PortValue{}
		}
		port, source, err := PortFromLaunch(name, launch)
		if err != nil {
			return PortValue{}
		}
		return PortValue{Port: port, Source: source}
	}

	return ExternalPorts{
		Game:    discover("game", gameLaunch, gameErr),
		Monitor: discover("monitor", Launch{}, nil),
		Auction: discover("auction", Launch{}, nil),
		Point:   discover("point", Launch{}, nil),
		Relay:   discover("relay", Launch{}, nil),
	}
}

func PortFromLaunch(service string, launch Launch) (int, string, error) {
	if port, ok := portFromArgs(launch.Args); ok {
		return port, launch.Source + " command", nil
	}
	path, err := ConfigPath(launch)
	if err != nil {
		return 0, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	port, err := PortFromConfig(service, string(data))
	if err != nil {
		return 0, "", fmt.Errorf("%s: %w", path, err)
	}
	return port, path, nil
}

func portFromArgs(args []string) (int, bool) {
	for i, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		for _, prefix := range []string{"--port=", "-port=", "port="} {
			if strings.HasPrefix(lower, prefix) {
				return validPort(strings.TrimSpace(arg[len(prefix):]))
			}
		}
		if lower == "--port" || lower == "-port" || lower == "-p" {
			if i+1 < len(args) {
				return validPort(args[i+1])
			}
		}
	}
	return 0, false
}

var configPortPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9_.-]*port[a-z0-9_.-]*|port)\s*(?:=|:)\s*["']?(?:[a-z0-9_.-]+:)?([0-9]{1,5})`)
var spacedPortPattern = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9_.-]*port[a-z0-9_.-]*|port)\s+([0-9]{1,5})(?:\s|$)`)

func PortFromConfig(service, text string) (int, error) {
	type candidate struct {
		port  int
		score int
	}
	serviceKey := normalizePortKey(service + "_port")
	candidates := make([]candidate, 0)
	for _, line := range strings.Split(text, "\n") {
		line = stripConfigComment(line)
		matches := configPortPattern.FindAllStringSubmatch(line, -1)
		if match := spacedPortPattern.FindStringSubmatch(line); len(match) == 3 {
			matches = append(matches, match)
		}
		for _, match := range matches {
			port, ok := validPort(match[2])
			if !ok {
				continue
			}
			key := normalizePortKey(match[1])
			if isDatabasePortKey(key) {
				continue
			}
			score := 0
			switch key {
			case serviceKey, normalizePortKey(service + "port"):
				score = 3
			case "port", "listenport", "tcpport", "thistcpport", "serverport", "serviceport", "portno":
				score = 2
			default:
				if strings.Contains(key, normalizePortKey(service)) {
					score = 1
				}
			}
			if score > 0 {
				candidates = append(candidates, candidate{port: port, score: score})
			}
		}
	}
	bestScore := 0
	ports := map[int]bool{}
	for _, item := range candidates {
		if item.score > bestScore {
			bestScore = item.score
			ports = map[int]bool{item.port: true}
		} else if item.score == bestScore {
			ports[item.port] = true
		}
	}
	if bestScore == 0 || len(ports) == 0 {
		return 0, fmt.Errorf("listen port not found")
	}
	if len(ports) != 1 {
		return 0, fmt.Errorf("listen port is ambiguous")
	}
	for port := range ports {
		return port, nil
	}
	return 0, fmt.Errorf("listen port not found")
}

func isDatabasePortKey(key string) bool {
	return strings.Contains(key, "dbport") || strings.Contains(key, "databaseport")
}

func stripConfigComment(line string) string {
	quote := rune(0)
	for i, ch := range line {
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '#' || ch == ';' {
			return line[:i]
		}
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return line[:i]
		}
	}
	return line
}

func normalizePortKey(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, value)
}

func validPort(value string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}
