package service

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (m *RobotManager) loadMapCatalog() []mapCatalogItem {
	path := filepath.Join(m.cfg.ConfigDir, "pvf_map_catalog.json")
	mod := fileModTime(path)
	m.cacheMu.Lock()
	if m.mapCached && m.mapMod.Equal(mod) {
		maps := m.mapCache
		m.cacheMu.Unlock()
		return maps
	}
	m.cacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var maps []mapCatalogItem
	if json.Unmarshal(data, &maps) != nil {
		return nil
	}
	m.cacheMu.Lock()
	m.mapCache = maps
	m.mapMod = mod
	m.mapCached = true
	m.cacheMu.Unlock()
	return maps
}
