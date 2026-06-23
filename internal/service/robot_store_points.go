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
	storePointCacheVer  = 6
	storePointXStep     = 120
	storePointYStep     = 80
	storeRestrictHalfX  = 80
	storeRestrictHalfY  = 150
	storePointRegionX   = 180
	storePointRegionY   = 120
	storePointClaimTTL  = 2 * time.Minute
	storePointSaveMax   = 100
	storePointSaveAge   = 30 * time.Second
	storePointFailRetry = 6 * time.Minute
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
	Version    int              `json:"version"`
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
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (storePosition, bool) {
		return c.claimFromArea(uid, areaKey, true)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (storePosition, bool) {
		return c.claimFromArea(uid, areaKey, false)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (storePosition, bool) {
		return c.claimFailedFromArea(uid, areaKey, true)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (storePosition, bool) {
		return c.claimFailedFromArea(uid, areaKey, false)
	}); ok {
		return pos, true
	}
	return storePosition{}, false
}

func (c *storePointCoordinator) claimAcrossAreas(fn func(string) (storePosition, bool)) (storePosition, bool) {
	for scanned := 0; scanned < len(c.areaOrder); scanned++ {
		areaKey := c.areaOrder[c.areaCursor%len(c.areaOrder)]
		c.areaCursor = (c.areaCursor + 1) % len(c.areaOrder)
		if pos, ok := fn(areaKey); ok {
			return pos, true
		}
	}
	return storePosition{}, false
}

func (c *storePointCoordinator) claimFromArea(uid int, areaKey string, successOnly bool) (storePosition, bool) {
	now := time.Now()
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if c.failedPoints[pt.ID] {
			continue
		}
		if c.regionRecentlySucceeded(areaKey, pt.Region, now) {
			continue
		}
		if pt.LastReason == "store_failed" && c.recentFailedPoint(pt, now) {
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
		claim := storePointClaim{UID: uid, ExpiresAt: now.Add(storePointClaimTTL)}
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

func (c *storePointCoordinator) claimFailedFromArea(uid int, areaKey string, requireAreaSuccess bool) (storePosition, bool) {
	now := time.Now()
	if requireAreaSuccess && !c.areaHasUsableSuccess(areaKey, now) {
		return storePosition{}, false
	}
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if !c.failedPoints[pt.ID] || c.recentFailedPoint(pt, now) {
			continue
		}
		if c.regionRecentlySucceeded(areaKey, pt.Region, now) {
			continue
		}
		if _, ok := c.pointClaims[pt.ID]; ok {
			continue
		}
		if _, ok := c.regionClaims[pt.Region]; ok {
			continue
		}
		claim := storePointClaim{UID: uid, ExpiresAt: now.Add(storePointClaimTTL)}
		c.pointClaims[pt.ID] = claim
		c.regionClaims[pt.Region] = claim
		return storePosition{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: "grid_failed_retry", PointID: pt.ID, Region: pt.Region}, true
	}
	return storePosition{}, false
}

func (c *storePointCoordinator) areaHasUsableSuccess(areaKey string, now time.Time) bool {
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if !c.successPoints[pt.ID] {
			continue
		}
		if pt.LastReason == "store_failed" && c.recentFailedPoint(pt, now) {
			continue
		}
		return true
	}
	return false
}

func (c *storePointCoordinator) regionRecentlySucceeded(areaKey, region string, now time.Time) bool {
	if region == "" {
		return false
	}
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if pt.Region != region || pt.LastReason != "store_ack" {
			continue
		}
		last, err := time.Parse(time.RFC3339, pt.LastResultAt)
		if err != nil {
			continue
		}
		if now.Sub(last) < storePointClaimTTL {
			return true
		}
	}
	return false
}

func (c *storePointCoordinator) recentFailedPoint(pt storeGridPoint, now time.Time) bool {
	if pt.LastResultAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, pt.LastResultAt)
	if err != nil {
		return true
	}
	return now.Sub(last) < storePointFailRetry
}

func (c *storePointCoordinator) report(uid int, pos storePosition, try int, ok bool, reason string) {
	if pos.PointID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		claim := storePointClaim{UID: uid, ExpiresAt: time.Now().Add(storePointClaimTTL)}
		c.pointClaims[pos.PointID] = claim
		if pos.Region != "" {
			c.regionClaims[pos.Region] = claim
		}
	} else {
		delete(c.pointClaims, pos.PointID)
		delete(c.regionClaims, pos.Region)
	}
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
		if hasPoint {
			if c.points[idx].Success > 0 {
				c.successPoints[pos.PointID] = true
				c.points[idx].Status = "success"
			} else {
				c.failedPoints[pos.PointID] = true
				c.points[idx].Status = "failed"
			}
			c.points[idx].Failed++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		} else {
			c.failedPoints[pos.PointID] = true
		}
		if reason == "store_err_0x52" {
			c.markRestrictiveZoneLocked(uid, pos, now)
		}
	}
	c.dirtyCount++
	if c.dirtyCount >= storePointSaveMax || time.Since(c.lastCacheSave) >= storePointSaveAge {
		c.saveCacheLocked()
	}
}

func (c *storePointCoordinator) markRestrictiveZoneLocked(uid int, pos storePosition, now string) {
	areaKey := storeAreaKey(pos.Village, pos.Area)
	maxDX := storeRestrictHalfX * 2
	maxDY := storeRestrictHalfY * 2
	for _, idx := range c.byArea[areaKey] {
		pt := &c.points[idx]
		if pt.Success > 0 || pt.Status == "success" {
			continue
		}
		if absInt(pt.X-pos.X) > maxDX || absInt(pt.Y-pos.Y) > maxDY {
			continue
		}
		c.failedPoints[pt.ID] = true
		c.triedPoints[pt.ID] = true
		pt.Status = "failed"
		pt.LastUID = uid
		pt.LastReason = "store_err_0x52_zone"
		pt.LastResultAt = now
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
		Version:    storePointCacheVer,
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
	sourceName := "pvf_map_catalog.json"
	sourceData, err := os.ReadFile(filepath.Join(c.configDir, sourceName))
	if err != nil {
		return err
	}
	sum := md5.Sum(sourceData)
	sourceMD5 := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(c.configDir, storePointCacheFile)
	if cacheData, err := os.ReadFile(cachePath); err == nil {
		var cache storePointCache
		if json.Unmarshal(cacheData, &cache) == nil &&
			cache.Version == storePointCacheVer &&
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
		Version: storePointCacheVer, SourceFile: sourceName, SourceMD5: sourceMD5, XStep: storePointXStep, YStep: storePointYStep,
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
		if pt.Success > 0 || pt.Status == "success" {
			c.successPoints[pt.ID] = true
			continue
		}
		if pt.Status == "failed" {
			c.failedPoints[pt.ID] = true
		}
	}
	c.areaOrder = c.areaOrder[:0]
	for key := range c.byArea {
		c.areaOrder = append(c.areaOrder, key)
	}
	sort.Slice(c.areaOrder, func(i, j int) bool {
		pi, pj := storeAreaPriority[c.areaOrder[i]], storeAreaPriority[c.areaOrder[j]]
		if pi != pj {
			return pi > pj
		}
		return c.areaOrder[i] < c.areaOrder[j]
	})
}

func buildStoreGridPoints(maps []mapCatalogItem) []storeGridPoint {
	var points []storeGridPoint
	skippedArea := 0
	for _, mp := range maps {
		if !mp.Use || mp.XMax < mp.XMin || mp.YMax < mp.YMin {
			continue
		}
		if !isStoreAreaEligible(mp.Village, mp.Area) {
			skippedArea++
			continue
		}
		for y := storePointYStart(mp); y <= mp.YMax; y += storePointYStep {
			for x := mp.XMin; x <= mp.XMax; x += storePointXStep {
				region := storeRegionKey(mp.Village, mp.Area, x, y)
				points = append(points, storeGridPoint{
					ID:      fmt.Sprintf("%d-%d-%d-%d", mp.Village, mp.Area, x, y),
					Village: mp.Village, Area: mp.Area, X: x, Y: y, Region: region, Status: "unknown",
				})
			}
		}
	}
	if skippedArea > 0 {
		robotLogf("[StorePoint] filtered_invalid_areas=%d\n", skippedArea)
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

func storePointYStart(mp mapCatalogItem) int {
	if mp.YMax <= mp.YMin {
		return mp.YMin
	}
	return mp.YMin + (mp.YMax-mp.YMin)/2
}

func isStoreAreaEligible(village, area int) bool {
	key := [2]int{village, area}
	if storeGateArea[key] {
		return false
	}
	return storeAreaEligible[key]
}

var storeGateArea = map[[2]int]bool{
	{1, 1}: true, {2, 5}: true, {3, 2}: true, {4, 1}: true, {5, 1}: true,
	{6, 4}: true, {8, 1}: true, {9, 2}: true, {10, 1}: true, {11, 3}: true,
	{14, 3}: true, {15, 0}: true, {16, 0}: true, {17, 0}: true, {18, 0}: true,
	{19, 0}: true, {20, 0}: true, {21, 7}: true, {23, 0}: true, {24, 0}: true,
	{25, 0}: true, {26, 0}: true,
}

func storeGateAreaForVillage(village int) int {
	if area, ok := storeGateAreaByVillage[village]; ok {
		return area
	}
	return 1
}

var storeGateAreaByVillage = map[int]int{
	1: 1, 2: 5, 3: 2, 4: 1, 5: 1, 6: 4, 8: 1, 9: 2, 10: 1, 11: 3,
	14: 3, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 7, 23: 0,
	24: 0, 25: 0, 26: 0,
}

var storeAreaEligible = map[[2]int]bool{
	{1, 0}: true,
	{2, 0}: true, {2, 1}: true, {2, 2}: true, {2, 3}: true, {2, 8}: true,
	{3, 0}: true, {3, 1}: true, {3, 8}: true,
	{4, 0}: true, {4, 5}: true,
	{5, 0}: true,
	{6, 0}: true, {6, 1}: true,
	{8, 0}: true,
	{9, 0}: true, {9, 3}: true,
	{10, 2}: true, {10, 3}: true,
	{11, 0}: true, {11, 1}: true,
	{14, 0}: true, {14, 1}: true, {14, 4}: true, {14, 5}: true,
	{15, 1}: true, {15, 3}: true,
	{16, 1}: true,
	{17, 1}: true, {17, 3}: true, {17, 4}: true,
	{18, 1}: true,
	{19, 1}: true, {19, 3}: true,
	{20, 1}: true, {20, 2}: true,
	{21, 0}: true, {21, 1}: true, {21, 2}: true, {21, 3}: true,
	{23, 1}: true, {23, 3}: true, {23, 4}: true,
	{24, 1}: true,
	{25, 1}: true,
}

var storeAreaPriority = map[string]int{
	"4/0":  30,
	"5/0":  26,
	"6/0":  17,
	"2/8":  17,
	"2/0":  16,
	"2/1":  15,
	"8/0":  13,
	"3/8":  11,
	"9/0":  10,
	"3/0":  9,
	"2/2":  9,
	"10/2": 7,
	"3/1":  6,
	"1/0":  6,
	"6/1":  4,
	"25/1": 4,
	"2/3":  4,
	"4/5":  3,
	"11/1": 3,
	"23/3": 2,
	"16/1": 2,
	"11/0": 2,
}

func storeAreaKey(village, area int) string {
	return fmt.Sprintf("%d/%d", village, area)
}

func storeRegionKey(village, area, x, y int) string {
	return fmt.Sprintf("%d/%d/%d/%d", village, area, x/storePointRegionX, y/storePointRegionY)
}
