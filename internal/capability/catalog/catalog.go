package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/foundation/lockhub"
	"robot/internal/shared"
)

type partySkillCatalogFile struct {
	MaxSkillLevel int                      `json:"max_skill_level"`
	Skills        []partySkillCatalogEntry `json:"skills"`
}

type partySkillCatalogEntry struct {
	Disabled   bool   `json:"disabled,omitempty"`
	Job        int    `json:"job"`
	SkillIndex int    `json:"skill_index"`
	State      int    `json:"state"`
	Level      int    `json:"level"`
	Name       string `json:"name,omitempty"`
	ScriptPath string `json:"script_path,omitempty"`
	StateData  []int  `json:"state_data,omitempty"`
	Risk       int    `json:"risk,omitempty"`
}

func LoadPartySkills(configDir string) error {
	var cfg partySkillCatalogFile
	path := filepath.Join(configDir, "party_skill_catalog.json")
	if err := readJSON(path, &cfg); err != nil {
		return err
	}
	maxLevel := cfg.MaxSkillLevel
	if maxLevel <= 0 {
		maxLevel = 70
	}
	entries := make([]shared.PartySkillState, 0, len(cfg.Skills))
	for _, entry := range cfg.Skills {
		if entry.Disabled || entry.Job < 0 || entry.Level <= 0 || entry.Level > maxLevel || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		stateData, err := partySkillStateData(entry.StateData)
		if err != nil {
			return fmt.Errorf("party skill job=%d skill=%d state=%d: %w", entry.Job, entry.SkillIndex, entry.State, err)
		}
		entries = append(entries, shared.PartySkillState{
			Job: entry.Job, SkillIndex: entry.SkillIndex, State: entry.State,
			Level: entry.Level, Name: entry.Name, ScriptPath: entry.ScriptPath,
			StateData: stateData, Risk: entry.Risk,
		})
	}
	shared.SetPartySkillStates(entries)
	return nil
}

func partySkillStateData(values []int) ([]byte, error) {
	if len(values) > 3 {
		return nil, fmt.Errorf("state_data has %d values, maximum is 3", len(values))
	}
	data := make([]byte, 0, len(values)*3)
	for _, value := range values {
		if value < 0 || value > 0xffffff {
			return nil, fmt.Errorf("state_data value %d is outside 0..16777215", value)
		}
		data = append(data, byte(value), byte(value>>8), byte(value>>16))
	}
	return data, nil
}

func Equipment(configDir string) []shared.EquipmentCatalogItem {
	return equipmentFile(configDir, "pvf_equipment_catalog.json")
}

func Stackable(configDir string) []shared.EquipmentCatalogItem {
	return equipmentFile(configDir, "pvf_stackable_catalog.json")
}

type jsonFileStamp struct {
	exists  bool
	mtimeNS int64
	size    int64
}

type jsonFileCacheEntry[T any] struct {
	stamp jsonFileStamp
	value T
}

type jsonFileCache[T any] struct {
	mu      lockhub.Locker
	entries map[string]jsonFileCacheEntry[T]
}

var (
	mapCatalogFiles jsonFileCache[[]shared.MapCatalogItem]
	shoutFiles      jsonFileCache[robottemplate.ShoutTemplates]
)

func Maps(configDir string) []shared.MapCatalogItem {
	path := filepath.Join(configDir, "pvf_map_catalog.json")
	maps := mapCatalogFiles.load(path, nil, func(data []byte, fallback []shared.MapCatalogItem) []shared.MapCatalogItem {
		var out []shared.MapCatalogItem
		if json.Unmarshal(data, &out) != nil {
			return fallback
		}
		return out
	})
	return append([]shared.MapCatalogItem(nil), maps...)
}

func ShoutTemplates(configDir string) robottemplate.ShoutTemplates {
	fallback := robottemplate.ShoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}
	path := filepath.Join(configDir, "robot_shout_templates.json")
	t := shoutFiles.load(path, fallback, decodeShoutTemplates)
	return robottemplate.CloneShoutTemplates(t)
}

func decodeShoutTemplates(data []byte, fallback robottemplate.ShoutTemplates) robottemplate.ShoutTemplates {
	t := fallback
	var messages []string
	if json.Unmarshal(data, &messages) == nil {
		t.Messages = robottemplate.DedupeStrings(messages)
	} else {
		_ = json.Unmarshal(data, &t)
		t.Messages = robottemplate.DedupeStrings(t.Messages)
	}
	if t.Type == 0 {
		t.Type = 3
	}
	if len(t.Messages) == 0 {
		t.Messages = []string{"hello"}
	}
	return t
}

func (c *jsonFileCache[T]) load(path string, fallback T, decode func([]byte, T) T) T {
	path = canonicalCatalogPath(path)
	stamp, err := catalogFileStamp(path)
	if err != nil {
		return fallback
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[path]; ok && entry.stamp == stamp {
		return entry.value
	}

	value := fallback
	if stamp.exists {
		data, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			value = decode(data, fallback)
		case os.IsNotExist(readErr):
			stamp = jsonFileStamp{}
		default:
			return fallback
		}
	}
	if c.entries == nil {
		c.entries = make(map[string]jsonFileCacheEntry[T])
	}
	c.entries[path] = jsonFileCacheEntry[T]{stamp: stamp, value: value}
	return value
}

func canonicalCatalogPath(path string) string {
	clean := filepath.Clean(path)
	if absolute, err := filepath.Abs(clean); err == nil {
		return absolute
	}
	return clean
}

func catalogFileStamp(path string) (jsonFileStamp, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return jsonFileStamp{}, nil
	}
	if err != nil {
		return jsonFileStamp{}, err
	}
	return jsonFileStamp{exists: true, mtimeNS: info.ModTime().UnixNano(), size: info.Size()}, nil
}

func NameTemplates(configDir string) robottemplate.NameTemplates {
	t := robottemplate.NameTemplates{
		Prefixes:  []string{"Bot", "Star", "Moon", "Sky"},
		Middles:   []string{"Blade", "Wind", "Light", "Fire"},
		Suffixes:  []string{"One", "Two", "X", "Z"},
		Pattern:   "{prefix}{middle}{suffix}{number}",
		NumberMin: 10,
		NumberMax: 99,
	}
	data, err := os.ReadFile(filepath.Join(configDir, "robot_name_templates.json"))
	if err == nil {
		if names := robottemplate.ParseStringListJSON(data); len(names) > 0 {
			t.Names = names
		} else {
			_ = json.Unmarshal(data, &t)
		}
		t.Names = robottemplate.DedupeStrings(t.Names)
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

func equipmentFile(configDir string, name string) []shared.EquipmentCatalogItem {
	var out []shared.EquipmentCatalogItem
	_ = readJSON(filepath.Join(configDir, name), &out)
	return out
}

func readJSON(path string, out interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
