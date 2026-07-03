package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"

	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/shared"
)

func Equipment(configDir string) []shared.EquipmentCatalogItem {
	return equipmentFile(configDir, "pvf_equipment_catalog.json")
}

func Stackable(configDir string) []shared.EquipmentCatalogItem {
	return equipmentFile(configDir, "pvf_stackable_catalog.json")
}

func Maps(configDir string) []shared.MapCatalogItem {
	var out []shared.MapCatalogItem
	_ = readJSON(filepath.Join(configDir, "pvf_map_catalog.json"), &out)
	return out
}

func ShoutTemplates(configDir string) robottemplate.ShoutTemplates {
	t := robottemplate.ShoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}
	data, err := os.ReadFile(filepath.Join(configDir, "robot_shout_templates.json"))
	if err == nil {
		var messages []string
		if json.Unmarshal(data, &messages) == nil {
			t.Messages = robottemplate.DedupeStrings(messages)
		} else {
			_ = json.Unmarshal(data, &t)
			t.Messages = robottemplate.DedupeStrings(t.Messages)
		}
	}
	if t.Type == 0 {
		t.Type = 3
	}
	if len(t.Messages) == 0 {
		t.Messages = []string{"hello"}
	}
	return robottemplate.CloneShoutTemplates(t)
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
