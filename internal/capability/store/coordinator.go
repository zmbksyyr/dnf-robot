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

	"robot/internal/foundation/lockhub"
	"robot/internal/foundation/mathx"
	"robot/internal/shared"
)

const (
	pointClaimTTL  = 2 * time.Minute
	pointSaveMax   = 100
	pointSaveAge   = 30 * time.Second
	PointFailRetry = 6 * time.Minute
)

const (
	PointStatusUnknown = "unknown"
	PointStatusSuccess = "success"
	PointStatusFailed  = "failed"
)

const (
	PointSourceUnknown     = "grid_unknown"
	PointSourceSuccess     = "grid_success"
	PointSourceFailedRetry = "grid_failed_retry"
)

const (
	StoreReasonAck                 = "store_ack"
	StoreReasonFailed              = "store_failed"
	StoreReasonOnlineFailed        = "store_online_failed"
	StoreReasonOnlineAttemptFailed = "online_failed"
	StoreReasonStartFailed         = "store_start_failed"
	StoreReasonNotConfirmed        = "store_not_confirmed"
	StoreReasonPrepareFailed       = "prepare_failed"
	StoreReasonSetAreaFailed       = "set_area_failed"
	StoreReasonCancelled           = "cancelled"
	StoreReasonRuntimeStopped      = "runtime_stopped"
	StoreReasonDisplayWaitFailed   = "display_wait_failed"
	StoreReasonErr011              = "store_err_0x11"
	StoreReasonErr052              = "store_err_0x52"
	StoreReasonErr052Zone          = "store_err_0x52_zone"
)

type Position struct {
	Village int
	Area    int
	X       int
	Y       int
	Source  string
	PointID string
	Region  string
}

type PointCoordinator struct {
	pointMu       lockhub.Locker
	configDir     string
	sourceName    string
	sourceMD5     string
	generatedAt   string
	points        []GridPoint
	byID          map[string]int
	byArea        map[string][]int
	areaOrder     []string
	areaCursor    int
	regionClaims  map[string]pointClaim
	pointClaims   map[string]pointClaim
	failedPoints  map[string]bool
	successPoints map[string]bool
	triedPoints   map[string]bool
	dirtyCount    int
	lastCacheSave time.Time
	logf          func(string, ...interface{})
}

type pointClaim struct {
	UID       int
	ExpiresAt time.Time
}

func NewPointCoordinator(configDir string, logf func(string, ...interface{})) *PointCoordinator {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	c := &PointCoordinator{
		configDir:     configDir,
		byID:          make(map[string]int),
		byArea:        make(map[string][]int),
		regionClaims:  make(map[string]pointClaim),
		pointClaims:   make(map[string]pointClaim),
		failedPoints:  make(map[string]bool),
		successPoints: make(map[string]bool),
		triedPoints:   make(map[string]bool),
		lastCacheSave: time.Now(),
		logf:          logf,
	}
	if configDir != "" {
		if err := c.load(); err != nil {
			c.logf("[StorePoint] load_failed err=%v\n", err)
		}
	}
	return c
}

func (c *PointCoordinator) Claim(uid int) (Position, bool) {
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	c.clearExpiredClaims(time.Now())
	if len(c.areaOrder) == 0 {
		return Position{}, false
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (Position, bool) {
		return c.claimFromArea(uid, areaKey, true)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (Position, bool) {
		return c.claimFromArea(uid, areaKey, false)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (Position, bool) {
		return c.claimFailedFromArea(uid, areaKey, true)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(areaKey string) (Position, bool) {
		return c.claimFailedFromArea(uid, areaKey, false)
	}); ok {
		return pos, true
	}
	return Position{}, false
}

func (c *PointCoordinator) claimAcrossAreas(fn func(string) (Position, bool)) (Position, bool) {
	for scanned := 0; scanned < len(c.areaOrder); scanned++ {
		areaKey := c.areaOrder[c.areaCursor%len(c.areaOrder)]
		c.areaCursor = (c.areaCursor + 1) % len(c.areaOrder)
		if pos, ok := fn(areaKey); ok {
			return pos, true
		}
	}
	return Position{}, false
}

func (c *PointCoordinator) claimFromArea(uid int, areaKey string, successOnly bool) (Position, bool) {
	now := time.Now()
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if c.failedPoints[pt.ID] {
			continue
		}
		if c.regionRecentlySucceeded(areaKey, pt.Region, now) {
			continue
		}
		if pt.LastReason == StoreReasonFailed && c.recentFailedPoint(pt, now) {
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
		claim := pointClaim{UID: uid, ExpiresAt: now.Add(pointClaimTTL)}
		c.pointClaims[pt.ID] = claim
		c.regionClaims[pt.Region] = claim
		source := PointSourceUnknown
		if successOnly {
			source = PointSourceSuccess
		}
		return Position{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: source, PointID: pt.ID, Region: pt.Region}, true
	}
	return Position{}, false
}

func (c *PointCoordinator) claimFailedFromArea(uid int, areaKey string, requireAreaSuccess bool) (Position, bool) {
	now := time.Now()
	if requireAreaSuccess && !c.areaHasUsableSuccess(areaKey, now) {
		return Position{}, false
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
		claim := pointClaim{UID: uid, ExpiresAt: now.Add(pointClaimTTL)}
		c.pointClaims[pt.ID] = claim
		c.regionClaims[pt.Region] = claim
		return Position{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: PointSourceFailedRetry, PointID: pt.ID, Region: pt.Region}, true
	}
	return Position{}, false
}

func (c *PointCoordinator) areaHasUsableSuccess(areaKey string, now time.Time) bool {
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if !c.successPoints[pt.ID] {
			continue
		}
		if pt.LastReason == StoreReasonFailed && c.recentFailedPoint(pt, now) {
			continue
		}
		return true
	}
	return false
}

func (c *PointCoordinator) regionRecentlySucceeded(areaKey, region string, now time.Time) bool {
	if region == "" {
		return false
	}
	for _, idx := range c.byArea[areaKey] {
		pt := c.points[idx]
		if pt.Region != region || pt.LastReason != StoreReasonAck {
			continue
		}
		last, err := time.Parse(time.RFC3339, pt.LastResultAt)
		if err != nil {
			continue
		}
		if now.Sub(last) < pointClaimTTL {
			return true
		}
	}
	return false
}

func (c *PointCoordinator) recentFailedPoint(pt GridPoint, now time.Time) bool {
	if pt.LastResultAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, pt.LastResultAt)
	if err != nil {
		return true
	}
	return now.Sub(last) < PointFailRetry
}

func (c *PointCoordinator) Report(uid int, pos Position, try int, ok bool, reason string) {
	if pos.PointID == "" {
		return
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	if ok {
		claim := pointClaim{UID: uid, ExpiresAt: time.Now().Add(pointClaimTTL)}
		c.pointClaims[pos.PointID] = claim
		if pos.Region != "" {
			c.regionClaims[pos.Region] = claim
		}
	} else {
		delete(c.pointClaims, pos.PointID)
		delete(c.regionClaims, pos.Region)
	}
	penalty := pointPenaltyReason(reason)
	if ok || penalty {
		c.triedPoints[pos.PointID] = true
	}
	idx, hasPoint := c.byID[pos.PointID]
	now := time.Now().Format(time.RFC3339)
	if ok {
		c.successPoints[pos.PointID] = true
		if hasPoint {
			c.points[idx].Status = PointStatusSuccess
			c.points[idx].Success++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		}
	} else {
		if hasPoint {
			if !penalty {
				c.points[idx].LastUID = uid
				c.points[idx].LastReason = reason
				c.points[idx].LastResultAt = now
			} else if c.points[idx].Success > 0 {
				c.successPoints[pos.PointID] = true
				c.points[idx].Status = PointStatusSuccess
			} else {
				c.failedPoints[pos.PointID] = true
				c.points[idx].Status = PointStatusFailed
			}
			if penalty {
				c.points[idx].Failed++
				c.points[idx].LastUID = uid
				c.points[idx].LastReason = reason
				c.points[idx].LastResultAt = now
			}
		} else if penalty {
			c.failedPoints[pos.PointID] = true
		}
		if penalty && reason == StoreReasonErr052 {
			c.markRestrictiveZoneLocked(uid, pos, now)
		}
	}
	c.dirtyCount++
	if c.dirtyCount >= pointSaveMax || time.Since(c.lastCacheSave) >= pointSaveAge {
		c.saveCacheLocked()
	}
}

func (c *PointCoordinator) Release(uid int, pos Position) {
	if pos.PointID == "" {
		return
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	if claim, ok := c.pointClaims[pos.PointID]; ok && (uid <= 0 || claim.UID == uid) {
		delete(c.pointClaims, pos.PointID)
	}
	if pos.Region != "" {
		if claim, ok := c.regionClaims[pos.Region]; ok && (uid <= 0 || claim.UID == uid) {
			delete(c.regionClaims, pos.Region)
		}
	}
}

func pointPenaltyReason(reason string) bool {
	switch reason {
	case StoreReasonErr011:
		return false
	default:
		return true
	}
}

func (c *PointCoordinator) SuccessCount() int {
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	return len(c.successPoints)
}

func (c *PointCoordinator) markRestrictiveZoneLocked(uid int, pos Position, now string) {
	areaKey := AreaKey(pos.Village, pos.Area)
	maxDX := RestrictHalfX * 2
	maxDY := RestrictHalfY * 2
	for _, idx := range c.byArea[areaKey] {
		pt := &c.points[idx]
		if pt.Success > 0 || pt.Status == PointStatusSuccess {
			continue
		}
		if mathx.AbsInt(pt.X-pos.X) > maxDX || mathx.AbsInt(pt.Y-pos.Y) > maxDY {
			continue
		}
		c.failedPoints[pt.ID] = true
		c.triedPoints[pt.ID] = true
		pt.Status = PointStatusFailed
		pt.LastUID = uid
		pt.LastReason = StoreReasonErr052Zone
		pt.LastResultAt = now
	}
}

func (c *PointCoordinator) Flush() {
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	c.saveCacheLocked()
}

func (c *PointCoordinator) saveCacheLocked() {
	if c.configDir == "" || c.dirtyCount == 0 {
		return
	}
	if err := os.MkdirAll(c.configDir, 0755); err != nil {
		c.logf("[StorePoint] cache_mkdir_failed err=%v\n", err)
		return
	}
	cache := PointCache{
		Version:    PointCacheVer,
		SourceFile: c.sourceName,
		SourceMD5:  c.sourceMD5,
		XStep:      PointXStep,
		YStep:      PointYStep,
		RegionX:    PointRegionX,
		RegionY:    PointRegionY,
		Generated:  c.generatedAt,
		Updated:    time.Now().Format(time.RFC3339),
		Points:     c.points,
	}
	if cache.Generated == "" {
		cache.Generated = cache.Updated
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		c.logf("[StorePoint] cache_encode_failed err=%v\n", err)
		return
	}
	if err := os.WriteFile(filepath.Join(c.configDir, PointCacheFile), data, 0644); err != nil {
		c.logf("[StorePoint] cache_write_failed err=%v\n", err)
		return
	}
	c.dirtyCount = 0
	c.lastCacheSave = time.Now()
}

func (c *PointCoordinator) clearExpiredClaims(now time.Time) {
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

func (c *PointCoordinator) load() error {
	sourceName := "pvf_map_catalog.json"
	sourceData, err := os.ReadFile(filepath.Join(c.configDir, sourceName))
	if err != nil {
		return err
	}
	sum := md5.Sum(sourceData)
	sourceMD5 := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(c.configDir, PointCacheFile)
	if cacheData, err := os.ReadFile(cachePath); err == nil {
		var cache PointCache
		if json.Unmarshal(cacheData, &cache) == nil &&
			cache.Version == PointCacheVer &&
			cache.SourceMD5 == sourceMD5 &&
			cache.XStep == PointXStep &&
			cache.YStep == PointYStep &&
			cache.RegionX == PointRegionX &&
			cache.RegionY == PointRegionY &&
			len(cache.Points) > 0 {
			c.sourceName = cache.SourceFile
			c.sourceMD5 = cache.SourceMD5
			c.generatedAt = cache.Generated
			c.points = FilterEligibleGridPoints(cache.Points)
			if len(c.points) > 0 {
				c.rebuildIndexes()
				c.logf("[StorePoint] cache_loaded source=%s md5=%s points=%d raw_points=%d areas=%d tried=%d success=%d failed=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(cache.Points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints))
				return nil
			}
		}
	}
	var maps []shared.MapCatalogItem
	if err := json.Unmarshal(sourceData, &maps); err != nil {
		return err
	}
	points := BuildGridPoints(maps)
	if len(points) == 0 {
		return fmt.Errorf("no store points generated from %s", sourceName)
	}
	generatedAt := time.Now().Format(time.RFC3339)
	cache := PointCache{
		Version: PointCacheVer, SourceFile: sourceName, SourceMD5: sourceMD5, XStep: PointXStep, YStep: PointYStep,
		RegionX: PointRegionX, RegionY: PointRegionY, Generated: generatedAt, Points: points,
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
	c.logf("[StorePoint] cache_generated source=%s md5=%s points=%d areas=%d tried=%d success=%d failed=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints))
	return nil
}

func (c *PointCoordinator) rebuildIndexes() {
	c.byID = make(map[string]int, len(c.points))
	c.byArea = make(map[string][]int)
	c.failedPoints = make(map[string]bool)
	c.successPoints = make(map[string]bool)
	c.triedPoints = make(map[string]bool)
	for i, pt := range c.points {
		c.byID[pt.ID] = i
		key := AreaKey(pt.Village, pt.Area)
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
	c.areaOrder = c.areaOrder[:0]
	for key := range c.byArea {
		c.areaOrder = append(c.areaOrder, key)
	}
	sort.Slice(c.areaOrder, func(i, j int) bool {
		pi, pj := AreaPriority[c.areaOrder[i]], AreaPriority[c.areaOrder[j]]
		if pi != pj {
			return pi > pj
		}
		return c.areaOrder[i] < c.areaOrder[j]
	})
}
