package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

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
