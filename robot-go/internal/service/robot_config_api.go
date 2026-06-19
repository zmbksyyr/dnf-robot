package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"robot/internal/config"
)

func (m *RobotManager) RobotConfig() (RobotConfigResult, error) {
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	data, err := os.ReadFile(path)
	if err != nil {
		return RobotConfigResult{}, err
	}
	return RobotConfigResult{Path: path, Text: publicRobotConfigText(string(data)), Config: m.loadRobotConfig()}, nil
}

func (m *RobotManager) UpdateRobotConfig(req RobotConfigUpdateRequest) (RobotConfigResult, error) {
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	if strings.TrimSpace(req.Text) != "" {
		if _, err := config.LoadFromString(req.Text); err != nil {
			return RobotConfigResult{}, err
		}
		if err := os.WriteFile(path, []byte(req.Text), 0644); err != nil {
			return RobotConfigResult{}, err
		}
		m.invalidateRobotConfigCache()
	} else if len(req.Updates) > 0 {
		values := make(map[string]string, len(req.Updates))
		for key, value := range req.Updates {
			values[key] = fmt.Sprint(value)
		}
		if err := m.writeRobotConfigValues(values); err != nil {
			return RobotConfigResult{}, err
		}
	}
	return m.RobotConfig()
}

func (m *RobotManager) writeRobotConfigValues(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := updateINIText(string(data), values)
	if _, err := config.LoadFromString(text); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return err
	}
	m.invalidateRobotConfigCache()
	return nil
}

func updateINIText(text string, values map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	section := ""
	seen := make(map[string]bool, len(values))
	sectionLine := make(map[string]int)
	lastInSection := make(map[string]int)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if end := strings.IndexByte(trimmed, ']'); end > 1 {
				section = strings.TrimSpace(trimmed[1:end])
				sectionLine[section] = i
				lastInSection[section] = i
			}
			continue
		}
		if section != "" && trimmed != "" {
			lastInSection[section] = i
		}
		if section == "" || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if idx := strings.IndexByte(trimmed, '='); idx > 0 {
			key := strings.TrimSpace(trimmed[:idx])
			fullKey := section + "." + key
			value, ok := values[fullKey]
			if !ok {
				continue
			}
			prefix := line[:strings.Index(line, "=")+1]
			lines[i] = prefix + " " + value
			seen[fullKey] = true
		}
	}
	for fullKey, value := range values {
		if seen[fullKey] {
			continue
		}
		parts := strings.SplitN(fullKey, ".", 2)
		if len(parts) != 2 {
			continue
		}
		section, key := parts[0], parts[1]
		line := key + " = " + value
		if _, ok := sectionLine[section]; !ok {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, "["+section+"]", line)
			sectionLine[section] = len(lines) - 2
			lastInSection[section] = len(lines) - 1
			continue
		}
		insertAt := lastInSection[section] + 1
		lines = append(lines[:insertAt], append([]string{line}, lines[insertAt:]...)...)
		for s, idx := range sectionLine {
			if idx >= insertAt {
				sectionLine[s] = idx + 1
			}
		}
		for s, idx := range lastInSection {
			if idx >= insertAt {
				lastInSection[s] = idx + 1
			}
		}
		lastInSection[section] = insertAt
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func publicRobotConfigText(text string) string {
	hidden := map[string]bool{
		"auto.auto_game_port_stable_sec":       true,
		"auto.auto_game_port_check_timeout_ms": true,
		"auto.auto_move_interval_min_sec":      true,
		"auto.auto_move_interval_max_sec":      true,
		"auto.auto_shout_interval_min_sec":     true,
		"auto.auto_shout_interval_max_sec":     true,
		"auto.auto_store_probability_percent":  true,
		"auto.auto_store_interval_min_sec":     true,
		"auto.auto_store_interval_max_sec":     true,
		"auto.auto_store_duration_sec":         true,
		"auto.auto_store_tick_sec":             true,
		"auto.auto_store_max_position_tries":   true,
		"auto.auto_store_fail_cooldown_sec":    true,
		"scheduler.bad_recover_sec":            true,
		"scheduler.bad_failures":               true,
		"scheduler.metrics_interval_sec":       true,
		"scheduler.store_concurrent":           true,
		"scheduler.online_batch_size":          true,
		"scheduler.online_start_rate":          true,
		"scheduler.online_fill_timeout_sec":    true,
		"scheduler.breaker_abnormal_percent":   true,
		"scheduler.breaker_pause_sec":          true,
		"scheduler.breaker_release_batch":      true,
		"scheduler.breaker_floor_percent":      true,
		"scheduler.port_down_release_batch":    true,
		"system.actor_poll_ms":                 true,
		"system.manual_action_timeout_sec":     true,
		"system.packet_rate_per_sec":           true,
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if end := strings.IndexByte(trimmed, ']'); end > 1 {
				section = strings.TrimSpace(trimmed[1:end])
			}
			out = append(out, line)
			continue
		}
		if section != "" && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			if idx := strings.IndexByte(trimmed, '='); idx > 0 {
				key := strings.TrimSpace(trimmed[:idx])
				if hidden[section+"."+key] {
					continue
				}
			}
		}
		out = append(out, line)
	}
	result := strings.Join(out, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}
