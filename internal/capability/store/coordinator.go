package store

import (
	"time"

	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/lockhub"
	"robot/internal/foundation/mathx"
)

const (
	pointClaimTTL       = 2 * time.Minute
	pointCleanupMargin  = 30 * time.Second
	pointSaveMax        = 100
	pointSaveAge        = 30 * time.Second
	PointFailRetry      = 6 * time.Minute
	pointFailureBurst   = time.Minute
	pointEvidenceWindow = 2 * time.Minute
	pointEvidenceLimit  = 3
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
	StoreReasonDisjointAck         = "disjoint_ack"
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
	StoreReasonInventoryNotReady   = "store_inventory_not_ready"
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
}

type PointCoordinator struct {
	pointMu        lockhub.Locker
	cacheMu        lockhub.Locker
	flushMu        lockhub.Locker
	activeSave     activePointPersistence
	configDir      string
	sourcePath     string
	sourceName     string
	sourceMD5      string
	generatedAt    string
	points         []GridPoint
	byID           map[string]int
	byArea         map[areaKey][]int
	areaOrder      []areaKey
	areaCursor     int
	pointClaims    map[string]pointClaim
	pointOccupancy map[areaKey]map[occupancyCell]map[string]pointOccupancy
	pointEvidence  map[pointEvidenceKey]map[int]time.Time
	pointCooldown  map[string]time.Time
	packedPoints   map[string]bool
	failedPoints   map[string]bool
	successPoints  map[string]bool
	triedPoints    map[string]bool
	dirtyCount     int
	activeDirty    int
	lastCacheSave  time.Time
	logf           func(string, ...interface{})
}

type pointClaim struct {
	UID        int
	ExpiresAt  time.Time
	ClaimUntil time.Time
	Lease      time.Duration
	ReuseAfter time.Duration
}

func (c *PointCoordinator) HasArea(village, area int) bool {
	if c == nil {
		return false
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	return len(c.byArea[areaKey{village, area}]) > 0
}

func NewPointCoordinator(stateDir, mapCatalogPath string, logf func(string, ...interface{})) *PointCoordinator {
	return newPointCoordinator(stateDir, mapCatalogPath, logf)
}

func newPointCoordinator(configDir, sourcePath string, logf func(string, ...interface{})) *PointCoordinator {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	c := &PointCoordinator{
		configDir:      configDir,
		sourcePath:     sourcePath,
		byID:           make(map[string]int),
		byArea:         make(map[areaKey][]int),
		pointClaims:    make(map[string]pointClaim),
		pointOccupancy: make(map[areaKey]map[occupancyCell]map[string]pointOccupancy),
		pointEvidence:  make(map[pointEvidenceKey]map[int]time.Time),
		pointCooldown:  make(map[string]time.Time),
		packedPoints:   make(map[string]bool),
		failedPoints:   make(map[string]bool),
		successPoints:  make(map[string]bool),
		triedPoints:    make(map[string]bool),
		lastCacheSave:  time.Now(),
		logf:           logf,
	}
	if configDir != "" {
		if err := c.load(); err != nil {
			c.logf("[StorePoint] load_failed err=%v\n", err)
		}
	}
	return c
}

func (c *PointCoordinator) Claim(uid int) (Position, bool) {
	return c.ClaimWithLease(uid, pointClaimTTL)
}

func (c *PointCoordinator) ClaimWithLease(uid int, lease time.Duration) (Position, bool) {
	lease = normalizePointLease(lease)
	return c.claim(uid, lease, lease)
}

// ClaimForStore keeps cleanup ownership for the longest possible configured
// store lifetime while making a successful point reusable at this UID's exact
// staggered store expiry.
func (c *PointCoordinator) ClaimForStore(uid, storeDurationSec int) (Position, bool) {
	cleanupLease := StorePointLeaseDuration(storeDurationSec)
	reuseAfter := robotconfig.StoreDurationForUID(storeDurationSec, uid)
	if reuseAfter < 0 {
		reuseAfter = 0
	}
	return c.claim(uid, cleanupLease, reuseAfter)
}

func (c *PointCoordinator) claim(uid int, lease, reuseAfter time.Duration) (Position, bool) {
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	now := time.Now()
	lease = normalizePointLease(lease)
	c.clearExpiredClaims(now)
	if len(c.areaOrder) == 0 {
		return Position{}, false
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFromArea(uid, area, PointStatusSuccess, true, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFromArea(uid, area, PointStatusSuccess, false, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFromArea(uid, area, PointStatusUnknown, true, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFailedFromArea(uid, area, true, true, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFromArea(uid, area, PointStatusUnknown, false, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	if pos, ok := c.claimAcrossAreas(func(area areaKey) (Position, bool) {
		return c.claimFailedFromArea(uid, area, false, false, now, lease, reuseAfter)
	}); ok {
		return pos, true
	}
	return Position{}, false
}

func (c *PointCoordinator) claimAcrossAreas(fn func(areaKey) (Position, bool)) (Position, bool) {
	for scanned := 0; scanned < len(c.areaOrder); scanned++ {
		areaKey := c.areaOrder[c.areaCursor%len(c.areaOrder)]
		c.areaCursor = (c.areaCursor + 1) % len(c.areaOrder)
		if pos, ok := fn(areaKey); ok {
			return pos, true
		}
	}
	return Position{}, false
}

func (c *PointCoordinator) claimFromArea(uid int, area areaKey, status string, packedOnly bool, now time.Time, lease, reuseAfter time.Duration) (Position, bool) {
	for _, idx := range c.byArea[area] {
		pt := c.points[idx]
		if status == PointStatusSuccess {
			if !c.successPoints[pt.ID] {
				continue
			}
		} else if status == PointStatusUnknown && c.triedPoints[pt.ID] {
			continue
		}
		if packedOnly && !c.packedPoints[pt.ID] {
			continue
		}
		if c.failedPoints[pt.ID] {
			continue
		}
		if c.positionRecentlyOccupied(area, pt, now) {
			continue
		}
		if c.recentFailedPoint(pt, now, lease) {
			continue
		}
		claim := newPointClaim(uid, now, lease, reuseAfter)
		c.setPointClaimLocked(pt.ID, claim)
		source := PointSourceUnknown
		if status == PointStatusSuccess {
			source = PointSourceSuccess
		}
		return Position{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: source, PointID: pt.ID}, true
	}
	return Position{}, false
}

func (c *PointCoordinator) claimFailedFromArea(uid int, area areaKey, packedOnly, requireAreaSuccess bool, now time.Time, lease, reuseAfter time.Duration) (Position, bool) {
	if requireAreaSuccess && !c.areaHasUsableSuccess(area, now, lease) {
		return Position{}, false
	}
	for _, idx := range c.byArea[area] {
		pt := c.points[idx]
		if packedOnly && !c.packedPoints[pt.ID] {
			continue
		}
		if !c.failedPoints[pt.ID] || c.recentFailedPoint(pt, now, lease) {
			continue
		}
		if c.positionRecentlyOccupied(area, pt, now) {
			continue
		}
		claim := newPointClaim(uid, now, lease, reuseAfter)
		c.setPointClaimLocked(pt.ID, claim)
		return Position{Village: pt.Village, Area: pt.Area, X: pt.X, Y: pt.Y, Source: PointSourceFailedRetry, PointID: pt.ID}, true
	}
	return Position{}, false
}

func (c *PointCoordinator) areaHasUsableSuccess(area areaKey, now time.Time, lease time.Duration) bool {
	for _, idx := range c.byArea[area] {
		pt := c.points[idx]
		if !c.successPoints[pt.ID] {
			continue
		}
		if c.recentFailedPoint(pt, now, lease) {
			continue
		}
		return true
	}
	return false
}

func (c *PointCoordinator) recentFailedPoint(pt GridPoint, now time.Time, lease time.Duration) bool {
	if c.pointCoolingDownLocked(pt.ID, now) {
		return true
	}
	if !pointPenaltyReason(pt.LastReason) {
		return false
	}
	retry, permanent := pointFailureRetry(pt.LastReason, lease)
	if permanent {
		return true
	}
	if pt.LastResultAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, pt.LastResultAt)
	if err != nil {
		return true
	}
	return now.Sub(last) < retry
}

func (c *PointCoordinator) Report(uid int, pos Position, ok bool, reason string) {
	if pos.PointID == "" {
		return
	}
	c.pointMu.Lock()
	nowTime := time.Now()
	c.clearExpiredClaims(nowTime)
	existing, claimed := c.pointClaims[pos.PointID]
	if claimed && existing.UID != uid {
		c.pointMu.Unlock()
		return
	}
	ownedClaim := claimed && existing.UID == uid
	successReason := pointSuccessReason(reason)
	if ok {
		claim := existing
		if !ownedClaim {
			claim = newPointClaim(uid, nowTime, pointClaimTTL, pointClaimTTL)
		} else {
			claim.ExpiresAt = nowTime.Add(claim.Lease)
			claim.ClaimUntil = nowTime.Add(claim.Lease)
		}
		if successReason {
			claim.ClaimUntil = time.Time{}
		}
		c.setPointClaimLocked(pos.PointID, claim)
	} else {
		c.discardPositionLocked(uid, pos)
	}
	penalty := pointPenaltyReason(reason)
	if !ok && !penalty {
		c.pointMu.Unlock()
		return
	}
	if ok || penalty {
		c.triedPoints[pos.PointID] = true
	}
	idx, hasPoint := c.byID[pos.PointID]
	now := nowTime.Format(time.RFC3339)
	activeChanged := false
	if ok {
		c.clearPointEvidenceLocked(pos.PointID)
		delete(c.pointCooldown, pos.PointID)
		delete(c.failedPoints, pos.PointID)
		c.successPoints[pos.PointID] = true
		if successReason {
			claim := c.pointClaims[pos.PointID]
			c.setPointSuccessOccupancyLocked(pos.PointID, nowTime.Add(claim.ReuseAfter))
			activeChanged = true
		} else {
			activeChanged = c.clearPointSuccessOccupancyLocked(pos.PointID)
		}
		if hasPoint {
			c.points[idx].Status = PointStatusSuccess
			c.points[idx].Success++
			c.points[idx].LastUID = uid
			c.points[idx].LastReason = reason
			c.points[idx].LastResultAt = now
		}
	} else {
		if ownedClaim {
			activeChanged = c.clearPointSuccessOccupancyLocked(pos.PointID)
		}
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
		if penalty && restrictivePointReason(reason) {
			c.clearPointEvidenceLocked(pos.PointID)
			delete(c.pointCooldown, pos.PointID)
			c.markRestrictiveZoneLocked(uid, pos, now)
			c.rebuildPackedPointsLocked()
		}
	}
	c.dirtyCount++
	if activeChanged {
		c.activeDirty++
	}
	shouldSave := c.cacheSaveDueLocked()
	c.pointMu.Unlock()
	if activeChanged {
		c.scheduleActiveOccupancySave()
	}
	if shouldSave {
		c.saveCache()
	}
}

// Discard releases an unconfirmed point claim without changing persisted point
// history. It is used for cancellation and session-scoped failures.
func (c *PointCoordinator) Discard(uid int, pos Position) {
	if pos.PointID == "" {
		return
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	c.discardPositionLocked(uid, pos)
}

func (c *PointCoordinator) ReleaseUID(uid int) {
	if uid <= 0 {
		return
	}
	c.pointMu.Lock()
	releasedActive := 0
	for id, claim := range c.pointClaims {
		if claim.UID != uid {
			continue
		}
		c.clearPointClaimLocked(id)
		if c.clearPointSuccessOccupancyLocked(id) {
			releasedActive++
		}
	}
	if releasedActive > 0 {
		c.activeDirty += releasedActive
	}
	c.pointMu.Unlock()
	if releasedActive > 0 {
		c.scheduleActiveOccupancySave()
	}
}

func (c *PointCoordinator) discardPositionLocked(uid int, pos Position) {
	if claim, ok := c.pointClaims[pos.PointID]; ok && (uid <= 0 || claim.UID == uid) {
		c.clearPointClaimLocked(pos.PointID)
	}
}

func StorePointLeaseDuration(storeDurationSec int) time.Duration {
	maxDuration := robotconfig.MaxStoreDurationSec(storeDurationSec)
	return normalizePointLease(time.Duration(maxDuration)*time.Second + pointCleanupMargin)
}

func normalizePointLease(lease time.Duration) time.Duration {
	if lease < pointClaimTTL {
		return pointClaimTTL
	}
	return lease
}

func newPointClaim(uid int, now time.Time, lease, reuseAfter time.Duration) pointClaim {
	lease = normalizePointLease(lease)
	return pointClaim{
		UID:        uid,
		ExpiresAt:  now.Add(lease),
		ClaimUntil: now.Add(lease),
		Lease:      lease,
		ReuseAfter: reuseAfter,
	}
}

func pointPenaltyReason(reason string) bool {
	switch reason {
	case "store_err_0x38", "store_err_0x3e", StoreReasonErr052, StoreReasonErr052Zone,
		"disjoint_err_0x14", "disjoint_err_0x3e", "disjoint_err_0x52", "disjoint_err_0xbe":
		return true
	default:
		return false
	}
}

func pointFailureRetry(reason string, lease time.Duration) (time.Duration, bool) {
	switch reason {
	case StoreReasonErr052, StoreReasonErr052Zone, "disjoint_err_0x52":
		return 0, true
	case "store_err_0x38", "disjoint_err_0x14", "disjoint_err_0xbe":
		return normalizePointLease(lease), false
	default:
		return PointFailRetry, false
	}
}

func restrictivePointReason(reason string) bool {
	return reason == StoreReasonErr052 || reason == "disjoint_err_0x52"
}

func ambiguousPointFailureReason(reason string) bool {
	return pointPenaltyReason(reason) && !restrictivePointReason(reason) && reason != StoreReasonErr052Zone
}

func pointSuccessReason(reason string) bool {
	return reason == StoreReasonAck || reason == StoreReasonDisjointAck
}

func (c *PointCoordinator) SuccessCount() int {
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	return len(c.successPoints)
}

func (c *PointCoordinator) markRestrictiveZoneLocked(uid int, pos Position, now string) {
	area := areaKey{pos.Village, pos.Area}
	maxDX := RestrictHalfX * 2
	maxDY := RestrictHalfY * 2
	for _, idx := range c.byArea[area] {
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

func (c *PointCoordinator) clearExpiredClaims(now time.Time) {
	for id, claim := range c.pointClaims {
		if now.After(claim.ExpiresAt) {
			c.clearPointClaimLocked(id)
		}
	}
	for pointID, until := range c.pointCooldown {
		if !now.Before(until) {
			delete(c.pointCooldown, pointID)
		}
	}
}
