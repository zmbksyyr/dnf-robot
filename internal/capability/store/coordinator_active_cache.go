package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	ActivePointCacheFile = "store_points_active.json"
	activePointCacheVer  = 1
)

type ActivePointCache struct {
	Version     int                    `json:"version"`
	SourceMD5   string                 `json:"source_md5"`
	Updated     string                 `json:"updated_at"`
	Occupancies []ActivePointOccupancy `json:"occupancies"`
}

type ActivePointOccupancy struct {
	PointID string `json:"point_id"`
	Until   string `json:"until"`
}

func (c *PointCoordinator) saveActiveOccupancies() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.pointMu.Lock()
	if c.configDir == "" || c.activeDirty == 0 {
		c.pointMu.Unlock()
		return
	}
	dirty := c.activeDirty
	configDir := c.configDir
	now := time.Now()
	cache := ActivePointCache{
		Version:     activePointCacheVer,
		SourceMD5:   c.sourceMD5,
		Updated:     now.Format(time.RFC3339),
		Occupancies: c.activeOccupanciesLocked(now),
	}
	c.pointMu.Unlock()

	if err := os.MkdirAll(configDir, 0755); err != nil {
		c.logf("[StorePoint] active_cache_mkdir_failed err=%v\n", err)
		return
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		c.logf("[StorePoint] active_cache_encode_failed err=%v\n", err)
		return
	}
	cachePath := filepath.Join(configDir, ActivePointCacheFile)
	tempPath := cachePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		c.logf("[StorePoint] active_cache_write_failed err=%v\n", err)
		return
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		c.logf("[StorePoint] active_cache_replace_failed err=%v\n", err)
		return
	}
	c.pointMu.Lock()
	c.activeDirty -= dirty
	if c.activeDirty < 0 {
		c.activeDirty = 0
	}
	c.pointMu.Unlock()
}

func (c *PointCoordinator) loadActiveOccupancies() int {
	data, err := os.ReadFile(filepath.Join(c.configDir, ActivePointCacheFile))
	if err != nil {
		return 0
	}
	var cache ActivePointCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != activePointCacheVer || cache.SourceMD5 != c.sourceMD5 {
		return 0
	}
	return c.restoreActiveOccupanciesLocked(cache.Occupancies, time.Now())
}
