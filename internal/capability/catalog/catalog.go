package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"

	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/foundation/lockhub"
	"robot/internal/shared"
)

// ItemCatalogView exposes the cached catalogs to audited read-only callers.
// The slices and their nested values must not be modified.
type ItemCatalogView struct {
	Equipment []shared.EquipmentCatalogItem
	Stackable []shared.EquipmentCatalogItem
}

func ViewItemCatalogs(configDir string) ItemCatalogView {
	return ItemCatalogView{
		Equipment: equipmentFileView(configDir, "pvf_equipment_catalog.json"),
		Stackable: equipmentFileView(configDir, "pvf_stackable_catalog.json"),
	}
}

func ViewStackable(configDir string) []shared.EquipmentCatalogItem {
	return equipmentFileView(configDir, "pvf_stackable_catalog.json")
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
	mapCatalogFiles  jsonFileCache[[]shared.MapCatalogItem]
	shoutFiles       jsonFileCache[robottemplate.ShoutTemplates]
	nameFiles        jsonFileCache[robottemplate.NameTemplates]
	itemCatalogFiles jsonFileCache[[]shared.EquipmentCatalogItem]
)

// ViewMaps returns the cached map catalog for audited read-only callers.
func ViewMaps(configDir string) []shared.MapCatalogItem {
	path := filepath.Join(configDir, "pvf_map_catalog.json")
	return mapCatalogFiles.load(path, nil, func(data []byte, fallback []shared.MapCatalogItem) []shared.MapCatalogItem {
		var out []shared.MapCatalogItem
		if json.Unmarshal(data, &out) != nil {
			return fallback
		}
		return out
	})
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

// NameTemplates returns a cached read-only template value. Callers must not
// modify the nested slices.
func NameTemplates(configDir string) robottemplate.NameTemplates {
	fallback := defaultNameTemplates()
	path := filepath.Join(configDir, "robot_name_templates.json")
	return nameFiles.load(path, fallback, decodeNameTemplates)
}

func defaultNameTemplates() robottemplate.NameTemplates {
	return robottemplate.NameTemplates{
		Prefixes:  []string{"Bot", "Star", "Moon", "Sky"},
		Middles:   []string{"Blade", "Wind", "Light", "Fire"},
		Suffixes:  []string{"One", "Two", "X", "Z"},
		Pattern:   "{prefix}{middle}{suffix}{number}",
		NumberMin: 10,
		NumberMax: 99,
	}
}

func decodeNameTemplates(data []byte, fallback robottemplate.NameTemplates) robottemplate.NameTemplates {
	t := fallback
	if names := robottemplate.ParseStringListJSON(data); len(names) > 0 {
		t.Names = names
	} else {
		_ = json.Unmarshal(data, &t)
	}
	t.Names = robottemplate.DedupeStrings(t.Names)
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

func equipmentFileView(configDir string, name string) []shared.EquipmentCatalogItem {
	path := filepath.Join(configDir, name)
	return itemCatalogFiles.load(path, nil, func(data []byte, fallback []shared.EquipmentCatalogItem) []shared.EquipmentCatalogItem {
		var out []shared.EquipmentCatalogItem
		if json.Unmarshal(data, &out) != nil {
			return fallback
		}
		return out
	})
}
