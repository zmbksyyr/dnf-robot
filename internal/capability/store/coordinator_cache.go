package store

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"robot/internal/shared"
)

func (c *PointCoordinator) Flush() {
	c.saveCache()
	c.saveActiveOccupancies()
}

func (c *PointCoordinator) cacheSaveDueLocked() bool {
	return c.configDir != "" && c.dirtyCount > 0 &&
		(c.dirtyCount >= pointSaveMax || time.Since(c.lastCacheSave) >= pointSaveAge)
}

func (c *PointCoordinator) saveCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.pointMu.Lock()
	if c.configDir == "" || c.dirtyCount == 0 {
		c.pointMu.Unlock()
		return
	}
	dirtyCount := c.dirtyCount
	configDir := c.configDir
	cache := PointCache{
		Version:    PointCacheVer,
		SourceFile: c.sourceName,
		SourceMD5:  c.sourceMD5,
		XStep:      PointXStep,
		YStep:      PointYStep,
		Generated:  c.generatedAt,
		Updated:    time.Now().Format(time.RFC3339),
		Points:     append([]GridPoint(nil), c.points...),
	}
	c.pointMu.Unlock()

	if err := os.MkdirAll(configDir, 0755); err != nil {
		c.logf("[StorePoint] cache_mkdir_failed err=%v\n", err)
		return
	}
	if cache.Generated == "" {
		cache.Generated = cache.Updated
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		c.logf("[StorePoint] cache_encode_failed err=%v\n", err)
		return
	}
	cachePath := filepath.Join(configDir, PointCacheFile)
	tempPath := cachePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		c.logf("[StorePoint] cache_write_failed err=%v\n", err)
		return
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		c.logf("[StorePoint] cache_replace_failed err=%v\n", err)
		return
	}
	c.pointMu.Lock()
	c.dirtyCount -= dirtyCount
	if c.dirtyCount < 0 {
		c.dirtyCount = 0
	}
	c.lastCacheSave = time.Now()
	c.pointMu.Unlock()
}

func (c *PointCoordinator) load() error {
	sourceName := "pvf_map_catalog.json"
	sourceData, err := os.ReadFile(filepath.Join(c.configDir, sourceName))
	if err != nil {
		return err
	}
	sum := md5.Sum(sourceData)
	sourceMD5 := hex.EncodeToString(sum[:])
	var maps []shared.MapCatalogItem
	if err := json.Unmarshal(sourceData, &maps); err != nil {
		return err
	}
	cachePath := filepath.Join(c.configDir, PointCacheFile)
	if cacheData, err := os.ReadFile(cachePath); err == nil {
		var cache PointCache
		if json.Unmarshal(cacheData, &cache) == nil &&
			cache.Version == PointCacheVer &&
			cache.SourceMD5 == sourceMD5 &&
			cache.XStep == PointXStep &&
			cache.YStep == PointYStep &&
			len(cache.Points) > 0 {
			c.sourceName = cache.SourceFile
			c.sourceMD5 = cache.SourceMD5
			c.generatedAt = cache.Generated
			c.points = FilterEligibleGridPoints(cache.Points, maps)
			if len(c.points) > 0 {
				c.rebuildIndexes()
				active := c.loadActiveOccupancies()
				c.logf("[StorePoint] cache_loaded source=%s md5=%s points=%d raw_points=%d areas=%d tried=%d success=%d failed=%d active=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(cache.Points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints), active)
				return nil
			}
		}
	}
	points := BuildGridPoints(maps)
	if len(points) == 0 {
		return fmt.Errorf("no store points generated from %s", sourceName)
	}
	generatedAt := time.Now().Format(time.RFC3339)
	cache := PointCache{
		Version: PointCacheVer, SourceFile: sourceName, SourceMD5: sourceMD5, XStep: PointXStep, YStep: PointYStep,
		Generated: generatedAt, Points: points,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return err
	}
	c.sourceName = sourceName
	c.sourceMD5 = sourceMD5
	c.generatedAt = generatedAt
	c.points = points
	c.rebuildIndexes()
	active := c.loadActiveOccupancies()
	c.logf("[StorePoint] cache_generated source=%s md5=%s points=%d areas=%d tried=%d success=%d failed=%d active=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints), active)
	return nil
}

func (c *PointCoordinator) rebuildIndexes() {
	if pruned := normalizeAmbiguousFailureBursts(c.points); pruned > 0 {
		c.dirtyCount += pruned
		c.logf("[StorePoint] session_failure_bursts_pruned points=%d\n", pruned)
	}
	c.byID = make(map[string]int, len(c.points))
	c.byArea = make(map[areaKey][]int)
	c.pointOccupancy = make(map[areaKey]map[occupancyCell]map[string]pointOccupancy)
	c.failedPoints = make(map[string]bool)
	c.successPoints = make(map[string]bool)
	c.triedPoints = make(map[string]bool)
	for i, pt := range c.points {
		c.byID[pt.ID] = i
		key := areaKey{pt.Village, pt.Area}
		c.byArea[key] = append(c.byArea[key], i)
		if pt.Success > 0 || pt.Status == PointStatusSuccess || pt.Status == PointStatusFailed {
			c.triedPoints[pt.ID] = true
		}
		if pt.Success > 0 || pt.Status == PointStatusSuccess {
			c.successPoints[pt.ID] = true
			continue
		}
		if pt.Status == PointStatusFailed {
			c.failedPoints[pt.ID] = true
		}
	}
	c.rebuildPackedPointsLocked()
	c.areaOrder = c.areaOrder[:0]
	for key := range c.byArea {
		c.areaOrder = append(c.areaOrder, key)
	}
	sort.Slice(c.areaOrder, func(i, j int) bool {
		if c.areaOrder[i][0] != c.areaOrder[j][0] {
			return c.areaOrder[i][0] < c.areaOrder[j][0]
		}
		return c.areaOrder[i][1] < c.areaOrder[j][1]
	})
}
