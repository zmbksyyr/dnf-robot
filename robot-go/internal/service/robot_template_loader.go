package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func (m *RobotManager) loadShoutTemplates() shoutTemplates {
	path := filepath.Join(m.cfg.ConfigDir, "robot_shout_templates.json")
	mod := fileModTime(path)
	m.cacheMu.Lock()
	if m.shoutCached && m.shoutMod.Equal(mod) {
		t := cloneShoutTemplates(m.shoutCache)
		m.cacheMu.Unlock()
		return t
	}
	m.cacheMu.Unlock()

	t := shoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}
	data, err := os.ReadFile(path)
	if err == nil {
		var messages []string
		if json.Unmarshal(data, &messages) == nil {
			t.Messages = dedupeStrings(messages)
		} else {
			_ = json.Unmarshal(data, &t)
			t.Messages = dedupeStrings(t.Messages)
		}
	}
	if t.Type == 0 {
		t.Type = 3
	}
	if len(t.Messages) == 0 {
		t.Messages = []string{"hello"}
	}
	m.cacheMu.Lock()
	m.shoutCache = cloneShoutTemplates(t)
	m.shoutMod = mod
	m.shoutCached = true
	m.cacheMu.Unlock()
	return cloneShoutTemplates(t)
}

func (m *RobotManager) loadNameTemplates() nameTemplates {
	t := nameTemplates{
		Prefixes:  []string{"Bot", "Star", "Moon", "Sky"},
		Middles:   []string{"Blade", "Wind", "Light", "Fire"},
		Suffixes:  []string{"One", "Two", "X", "Z"},
		Pattern:   "{prefix}{middle}{suffix}{number}",
		NumberMin: 10,
		NumberMax: 99,
	}
	data, err := os.ReadFile(filepath.Join(m.cfg.ConfigDir, "robot_name_templates.json"))
	if err == nil {
		if names := parseStringListJSON(data); len(names) > 0 {
			t.Names = names
		} else {
			_ = json.Unmarshal(data, &t)
		}
		t.Names = dedupeStrings(t.Names)
	}
	if len(t.Names) > 0 {
		return t
	}
	if len(t.Prefixes) == 0 {
		t.Prefixes = []string{"Bot"}
	}
	if len(t.Middles) == 0 {
		t.Middles = []string{"Name"}
	}
	if len(t.Suffixes) == 0 {
		t.Suffixes = []string{"X"}
	}
	if t.Pattern == "" {
		t.Pattern = "{prefix}{middle}{suffix}{number}"
	}
	return t
}

func cloneShoutTemplates(t shoutTemplates) shoutTemplates {
	t.Messages = append([]string(nil), t.Messages...)
	return t
}

func safeRobotShoutMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "hello"
	}
	const maxBytes = 72
	var b strings.Builder
	for _, r := range msg {
		if r < 0x20 {
			continue
		}
		next := string(r)
		if b.Len()+len(next) > maxBytes {
			break
		}
		b.WriteString(next)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "hello"
	}
	return out
}

func parseStringListJSON(data []byte) []string {
	var list []string
	if json.Unmarshal(data, &list) == nil {
		return dedupeStrings(list)
	}
	var obj struct {
		Names    []string `json:"names"`
		Messages []string `json:"messages"`
	}
	if json.Unmarshal(data, &obj) == nil {
		if len(obj.Names) > 0 {
			return dedupeStrings(obj.Names)
		}
		return dedupeStrings(obj.Messages)
	}
	return nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
