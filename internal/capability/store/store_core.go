package store

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/foundation/lockhub"
	"robot/internal/foundation/mathx"
	"robot/internal/shared"
	"sort"
	"strings"
	"time"
)

// ---- coordinator.go ----
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
	c.triedPoints[pos.PointID] = true
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
			if c.points[idx].Success > 0 {
				c.successPoints[pos.PointID] = true
				c.points[idx].Status = PointStatusSuccess
			} else {
				c.failedPoints[pos.PointID] = true
				c.points[idx].Status = PointStatusFailed
			}
			c.points[idx].Failed++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		} else {
			c.failedPoints[pos.PointID] = true
		}
		if reason == StoreReasonErr052 {
			c.markRestrictiveZoneLocked(uid, pos, now)
		}
	}
	c.dirtyCount++
	if c.dirtyCount >= pointSaveMax || time.Since(c.lastCacheSave) >= pointSaveAge {
		c.saveCacheLocked()
	}
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
			c.points = cache.Points
			c.rebuildIndexes()
			c.logf("[StorePoint] cache_loaded source=%s md5=%s points=%d areas=%d tried=%d success=%d failed=%d\n", c.sourceName, c.sourceMD5, len(c.points), len(c.areaOrder), len(c.triedPoints), len(c.successPoints), len(c.failedPoints))
			return nil
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
		if pt.Success > 0 || pt.Failed > 0 || (pt.Status != "" && pt.Status != PointStatusUnknown) {
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

// ---- grid.go ----
const (
	PointCacheFile = "store_points_cache.json"
	PointCacheVer  = 6
	PointXStep     = 120
	PointYStep     = 80
	RestrictHalfX  = 80
	RestrictHalfY  = 150
	PointRegionX   = 180
	PointRegionY   = 120
)

type GridPoint struct {
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

type PointCache struct {
	Version    int         `json:"version"`
	SourceFile string      `json:"source_file"`
	SourceMD5  string      `json:"source_md5"`
	XStep      int         `json:"x_step"`
	YStep      int         `json:"y_step"`
	RegionX    int         `json:"region_x"`
	RegionY    int         `json:"region_y"`
	Generated  string      `json:"generated_at"`
	Updated    string      `json:"updated_at,omitempty"`
	Points     []GridPoint `json:"points"`
}

func BuildGridPoints(maps []shared.MapCatalogItem) []GridPoint {
	var points []GridPoint
	for _, mp := range maps {
		if !mp.Use || mp.XMax < mp.XMin || mp.YMax < mp.YMin {
			continue
		}
		if !IsAreaEligible(mp.Village, mp.Area) {
			continue
		}
		for y := PointYStart(mp); y <= mp.YMax; y += PointYStep {
			for x := mp.XMin; x <= mp.XMax; x += PointXStep {
				region := RegionKey(mp.Village, mp.Area, x, y)
				points = append(points, GridPoint{
					ID:      fmt.Sprintf("%d-%d-%d-%d", mp.Village, mp.Area, x, y),
					Village: mp.Village, Area: mp.Area, X: x, Y: y, Region: region, Status: PointStatusUnknown,
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

func PointYStart(mp shared.MapCatalogItem) int {
	if mp.YMax <= mp.YMin {
		return mp.YMin
	}
	return mp.YMin + (mp.YMax-mp.YMin)/2
}

func IsAreaEligible(village, area int) bool {
	key := [2]int{village, area}
	if GateArea[key] {
		return false
	}
	return AreaEligible[key]
}

func GateAreaForVillage(village int) int {
	if area, ok := GateAreaByVillage[village]; ok {
		return area
	}
	return 1
}

func AreaKey(village, area int) string {
	return fmt.Sprintf("%d/%d", village, area)
}

func RegionKey(village, area, x, y int) string {
	return fmt.Sprintf("%d/%d/%d/%d", village, area, x/PointRegionX, y/PointRegionY)
}

var GateArea = map[[2]int]bool{
	{1, 1}: true, {2, 5}: true, {3, 2}: true, {4, 1}: true, {5, 1}: true,
	{6, 4}: true, {8, 1}: true, {9, 2}: true, {10, 1}: true, {11, 3}: true,
	{14, 3}: true, {15, 0}: true, {16, 0}: true, {17, 0}: true, {18, 0}: true,
	{19, 0}: true, {20, 0}: true, {21, 7}: true, {23, 0}: true, {24, 0}: true,
	{25, 0}: true, {26, 0}: true,
}

var GateAreaByVillage = map[int]int{
	1: 1, 2: 5, 3: 2, 4: 1, 5: 1, 6: 4, 8: 1, 9: 2, 10: 1, 11: 3,
	14: 3, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 7, 23: 0,
	24: 0, 25: 0, 26: 0,
}

var AreaEligible = map[[2]int]bool{
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

var AreaPriority = map[string]int{
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

// ---- inventory.go ----
const (
	WorldHornItemID   = 36
	WorldHornCount    = 200
	WorldHornBoxIndex = 55
	WorldHornRawIndex = WorldHornBoxIndex + 2
)

type InventoryPlan struct {
	Name     string
	StartBox int
}

type StallItem struct {
	ItemID int
	Count  int
	Price  int
}

type StallResult struct {
	StallRows  int
	ConfigRows int
}

type PermissionStatus struct {
	Premium    int
	Miles      int
	ProdUser   int
	PUUser     int
	EventEntry int
}

type ExpRepairResult struct {
	RefRows int
	Changed int64
}

func LevelMinExp(level int) (int, bool) {
	if level < 1 || level >= len(levelMinExpTable) {
		return 0, false
	}
	return levelMinExpTable[level], true
}

var levelMinExpTable = []int{
	0,
	0, 1000, 2653, 5543, 10575, 18509, 30205, 46627, 68840, 98012,
	135412, 182411, 240483, 311203, 396249, 497399, 619844, 767003, 942667, 1150141,
	1393864, 1677655, 2016592, 2419616, 2881880, 3410208, 4010357, 4690036, 5457538, 6319795,
	7286075, 8364071, 9564081, 10897068, 12372076, 14001186, 15794305, 17764684, 19926329, 22290671,
	24872971, 27685628, 30844672, 34385491, 38223728, 42379527, 46869144, 51714280, 56937632, 62557467,
	68598134, 75079161, 82244762, 90154153, 98612376, 107650320, 117292652, 127572249, 138523258, 150172894,
	162557394, 175705561, 190075639, 205759433, 222370655, 239953967, 258544759, 278190155, 298938852, 320829378,
	343912998, 368230186, 394571074, 423070174, 453854220, 487070095, 522855717, 561371046, 602783706, 647250436,
	694953116, 746061309, 801975949, 863016468, 929492724,
}

func InventoryPlanFor(startBox int) InventoryPlan {
	start := startBox
	if start <= 0 || start == 7 {
		start = 105
	}
	return InventoryPlan{Name: "material-default", StartBox: start}
}

func InventoryClearStartBoxes(start int) []int {
	if start == 105 {
		return []int{105}
	}
	return []int{start, 105}
}

func AttachAllowed(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	if attach == "" {
		return false
	}
	if strings.Contains(attach, "account") || strings.Contains(attach, "creature") || strings.Contains(attach, "unable") || strings.Contains(attach, "not") {
		return false
	}
	return strings.Contains(attach, "trade") || attach == "free" || attach == "sealing"
}

func AttachPreferred(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	return attach == "trade" || strings.Contains(attach, "trade ") || attach == "free" || attach == "sealing"
}

func InventoryTypeForBoxIndex(boxIndex int) int {
	switch {
	case boxIndex >= 7 && boxIndex <= 54:
		return 1
	case boxIndex >= 55 && boxIndex <= 102:
		return 2
	case boxIndex >= 103 && boxIndex <= 150:
		return 3
	case boxIndex >= 151 && boxIndex <= 198:
		return 4
	case boxIndex >= 199 && boxIndex <= 246:
		return 10
	default:
		return 2
	}
}

func InventoryTypeForStackable(item shared.EquipmentCatalogItem, fallback int) int {
	switch strings.ToLower(strings.TrimSpace(item.Slot)) {
	case "waste", "usable", "consumable":
		return 2
	case "material":
		return 3
	case "quest":
		return 4
	case "profession", "expert job":
		return 10
	default:
		return fallback
	}
}

func WriteInventoryStack(dst []byte, item shared.EquipmentCatalogItem, count int, inventoryType int) {
	if len(dst) < 61 {
		return
	}
	clear(dst)
	dst[0] = 0x00
	dst[1] = byte(inventoryType)
	binary.LittleEndian.PutUint32(dst[2:6], uint32(item.ID))
	binary.LittleEndian.PutUint32(dst[7:11], uint32(count))
}
