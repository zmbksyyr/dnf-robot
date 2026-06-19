package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	storePointCacheFile = "store_points_cache.json"
	storePointXStep     = 80
	storePointYStep     = 40
	storePointRegionX   = 120
	storePointRegionY   = 50
	storePointClaimTTL  = 45 * time.Second
	storePointSaveMax   = 100
	storePointSaveAge   = 30 * time.Second
)

type storePointCoordinator struct {
	mu            sync.Mutex
	configDir     string
	sourceName    string
	sourceMD5     string
	generatedAt   string
	points        []storeGridPoint
	byID          map[string]int
	byArea        map[string][]int
	areaOrder     []string
	areaCursor    int
	regionClaims  map[string]storePointClaim
	pointClaims   map[string]storePointClaim
	failedPoints  map[string]bool
	successPoints map[string]bool
	triedPoints   map[string]bool
	dirtyCount    int
	lastCacheSave time.Time
}

type storeGridPoint struct {
	ID           string `json:"id"`
	Village      int    `json:"village"`
	Area         int    `json:"area"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Region       string `json:"region"`
	Status       string `json:"status"`
	Success      int    `json:"success"`
	Failed       int    `json:"failed"`
	LastUID      int    `json:"last_uid,omitempty"`
	LastReason   string `json:"last_reason,omitempty"`
	LastResultAt string `json:"last_result_at,omitempty"`
}

type storePointCache struct {
	SourceFile string           `json:"source_file"`
	SourceMD5  string           `json:"source_md5"`
	XStep      int              `json:"x_step"`
	YStep      int              `json:"y_step"`
	RegionX    int              `json:"region_x"`
	RegionY    int              `json:"region_y"`
	Generated  string           `json:"generated_at"`
	Updated    string           `json:"updated_at,omitempty"`
	Points     []storeGridPoint `json:"points"`
}

type storePointClaim struct {
	UID       int
	ExpiresAt time.Time
}

func (m *RobotManager) storePoints() *storePointCoordinator {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storePointsCoord == nil {
		configDir := ""
		if m.cfg != nil {
			configDir = m.cfg.ConfigDir
		}
		m.storePointsCoord = newStorePointCoordinator(configDir)
	}
	return m.storePointsCoord
}

func newStorePointCoordinator(configDir string) *storePointCoordinator {
	c := &storePointCoordinator{
		configDir:     configDir,
		byID:          make(map[string]int),
		byArea:        make(map[string][]int),
		regionClaims:  make(map[string]storePointClaim),
		pointClaims:   make(map[string]storePointClaim),
		failedPoints:  make(map[string]bool),
		successPoints: make(map[string]bool),
		triedPoints:   make(map[string]bool),
		lastCacheSave: time.Now(),
	}
	if configDir != "" {
		if err := c.load(); err != nil {
			robotLogf("[StorePoint] load_failed err=%v\n", err)
		}
	}
	return c
}

func (c *storePointCoordinator) claim(uid int) (storePosition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearExpiredClaims(time.Now())
	if len(c.areaOrder) == 0 {
		return storePosition{}, false
	}
	for scanned := 0; scanned < len(c.areaOrder); scanned++ {
		areaKey := c.areaOrder[c.areaCursor%len(c.areaOrder)]
		c.areaCursor = (c.areaCursor + 1) % len(c.areaOrder)
		if pos, ok := c.claimFromArea(uid, areaKey, false); ok {
			return pos, true
		}
		if pos, ok := c.claimFromArea(uid, areaKey, true); ok {
			return pos, true
		}
	}
	return storePosition{}, false
}

func (c *storePointCoordinator) claimFromArea(uid int, areaKey string, successOnly bool) (storePosition, bool) {
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if c.failedPoints[pt.ID] {
			continue
		}
		if successOnly {
			if !c.successPoints[pt.ID] {
				continue
			}
		} else if c.triedPoints[pt.ID] {
			continue
		}
		if _, ok := c.pointClaims[pt.ID]; ok {
			continue
		}
		if _, ok := c.regionClaims[pt.Region]; ok {
			continue
		}
		claim := storePointClaim{UID: uid, ExpiresAt: time.Now().Add(storePointClaimTTL)}
		c.pointClaims[pt.ID] = claim
		c.regionClaims[pt.Region] = claim
		source := "grid_unknown"
		if successOnly {
			source = "grid_success"
		}
		return storePosition{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: source, PointID: pt.ID, Region: pt.Region}, true
	}
	return storePosition{}, false
}

func (c *storePointCoordinator) report(uid int, pos storePosition, try int, ok bool, reason string) {
	if pos.PointID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pointClaims, pos.PointID)
	delete(c.regionClaims, pos.Region)
	c.triedPoints[pos.PointID] = true
	idx, hasPoint := c.byID[pos.PointID]
	now := time.Now().Format(time.RFC3339)
	if ok {
		c.successPoints[pos.PointID] = true
		if hasPoint {
			c.points[idx].Status = "success"
			c.points[idx].Success++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		}
	} else {
		c.failedPoints[pos.PointID] = true
		delete(c.successPoints, pos.PointID)
		if hasPoint {
			c.points[idx].Status = "failed"
			c.points[idx].Failed++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		}
	}
	c.dirtyCount++
	if c.dirtyCount >= storePointSaveMax || time.Since(c.lastCacheSave) >= storePointSaveAge {
		c.saveCacheLocked()
	}
}

func (c *storePointCoordinator) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saveCacheLocked()
}

func (c *storePointCoordinator) saveCacheLocked() {
	if c.configDir == "" || c.dirtyCount == 0 {
		return
	}
	if err := os.MkdirAll(c.configDir, 0755); err != nil {
		robotLogf("[StorePoint] cache_mkdir_failed err=%v\n", err)
		return
	}
	cache := storePointCache{
		SourceFile: c.sourceName,
		SourceMD5:  c.sourceMD5,
		XStep:      storePointXStep,
		YStep:      storePointYStep,
		RegionX:    storePointRegionX,
		RegionY:    storePointRegionY,
		Generated:  c.generatedAt,
		Updated:    time.Now().Format(time.RFC3339),
		Points:     c.points,
	}
	if cache.Generated == "" {
		cache.Generated = cache.Updated
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		robotLogf("[StorePoint] cache_encode_failed err=%v\n", err)
		return
	}
	if err := os.WriteFile(filepath.Join(c.configDir, storePointCacheFile), data, 0644); err != nil {
		robotLogf("[StorePoint] cache_write_failed err=%v\n", err)
		return
	}
	c.dirtyCount = 0
	c.lastCacheSave = time.Now()
}

func (c *storePointCoordinator) clearExpiredClaims(now time.Time) {
	for id, claim := range c.pointClaims {
		if now.After(claim.ExpiresAt) {
			delete(c.pointClaims, id)
		}
	}
	for region, claim := range c.regionClaims {
		if now.After(claim.ExpiresAt) {
			delete(c.regionClaims, region)
		}
	}
}

func (c *storePointCoordinator) load() error {
	sourceName, sourceData, err := readConfigFileFallbackNamed(c.configDir, "pvf_map_catalog.json", "map_catalog.json", "store_map.json", "pvf_map.json")
	if err != nil {
		return err
	}
	sum := md5.Sum(sourceData)
	sourceMD5 := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(c.configDir, storePointCacheFile)
	if cacheData, err := os.ReadFile(cachePath); err == nil {
		var cache storePointCache
		if json.Unmarshal(cacheData, &cache) == nil &&
			cache.SourceMD5 == sourceMD5 &&
			cache.XStep == storePointXStep &&
			cache.YStep == storePointYStep &&
			cache.RegionX == storePointRegionX &&
			cache.RegionY == storePointRegionY &&
			len(cache.Points) > 0 {
			c.sourceName = cache.SourceFile
			c.sourceMD5 = cache.SourceMD5
			c.generatedAt = cache.Generated
			c.points = cache.Points
			c.rebuildIndexes()
			robotLogf("[StorePoint] cache_loaded source=%s md5=%s points=%d areas=%d tried=%d success=%d failed=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints))
			return nil
		}
	}
	var maps []mapCatalogItem
	if err := json.Unmarshal(sourceData, &maps); err != nil {
		return err
	}
	points := buildStoreGridPoints(maps)
	if len(points) == 0 {
		return fmt.Errorf("no store points generated from %s", sourceName)
	}
	generatedAt := time.Now().Format(time.RFC3339)
	cache := storePointCache{
		SourceFile: sourceName, SourceMD5: sourceMD5, XStep: storePointXStep, YStep: storePointYStep,
		RegionX: storePointRegionX, RegionY: storePointRegionY, Generated: generatedAt, Points: points,
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
	robotLogf("[StorePoint] cache_generated source=%s md5=%s points=%d areas=%d tried=%d success=%d failed=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints))
	return nil
}

func (c *storePointCoordinator) rebuildIndexes() {
	c.byID = make(map[string]int, len(c.points))
	c.byArea = make(map[string][]int)
	c.failedPoints = make(map[string]bool)
	c.successPoints = make(map[string]bool)
	c.triedPoints = make(map[string]bool)
	for i, pt := range c.points {
		c.byID[pt.ID] = i
		key := storeAreaKey(pt.Village, pt.Area)
		c.byArea[key] = append(c.byArea[key], i)
		if pt.Success > 0 || pt.Failed > 0 || (pt.Status != "" && pt.Status != "unknown") {
			c.triedPoints[pt.ID] = true
		}
		switch pt.Status {
		case "success":
			c.successPoints[pt.ID] = true
		case "failed":
			c.failedPoints[pt.ID] = true
		}
	}
	c.areaOrder = c.areaOrder[:0]
	for key := range c.byArea {
		c.areaOrder = append(c.areaOrder, key)
	}
	sort.Strings(c.areaOrder)
}

func buildStoreGridPoints(maps []mapCatalogItem) []storeGridPoint {
	var points []storeGridPoint
	for _, mp := range maps {
		if !mp.Use || mp.XMax < mp.XMin || mp.YMax < mp.YMin {
			continue
		}
		for y := mp.YMin; y <= mp.YMax; y += storePointYStep {
			for x := mp.XMin; x <= mp.XMax; x += storePointXStep {
				region := storeRegionKey(mp.Village, mp.Area, x, y)
				points = append(points, storeGridPoint{
					ID:      fmt.Sprintf("%d-%d-%d-%d", mp.Village, mp.Area, x, y),
					Village: mp.Village, Area: mp.Area, X: x, Y: y, Region: region, Status: "unknown",
				})
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.Village != b.Village {
			return a.Village < b.Village
		}
		if a.Area != b.Area {
			return a.Area < b.Area
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return points
}

func storeAreaKey(village, area int) string {
	return fmt.Sprintf("%d/%d", village, area)
}

func storeRegionKey(village, area, x, y int) string {
	return fmt.Sprintf("%d/%d/%d/%d", village, area, x/storePointRegionX, y/storePointRegionY)
}

func readConfigFileFallbackNamed(configDir string, names ...string) (string, []byte, error) {
	var lastErr error
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err == nil {
			return name, data, nil
		}
		lastErr = err
	}
	return "", nil, lastErr
}
