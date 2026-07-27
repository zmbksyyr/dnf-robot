package store

import "time"

type pointEvidenceKey struct {
	PointID string
	Reason  string
}

// AttemptFailureState keeps one coordinate failure provisional until another
// independent point confirms whether the failure belongs to the point or to
// the current account session.
type AttemptFailureState struct {
	position Position
	reason   string
}

// ReportAttemptFailure defers ambiguous point failures. If the same error is
// returned at a non-conflicting point, both observations are discarded because
// the current session, rather than both distant coordinates, is the common
// cause. The zero value of AttemptFailureState is ready for use.
func (c *PointCoordinator) ReportAttemptFailure(uid int, state *AttemptFailureState, pos Position, reason string) bool {
	if state == nil {
		if ambiguousPointFailureReason(reason) {
			c.rememberPointEvidence(uid, pos, reason)
			c.Discard(uid, pos)
			return false
		}
		c.Report(uid, pos, false, reason)
		return false
	}
	if state.reason != "" {
		if state.reason == reason && ambiguousPointFailureReason(reason) && !positionsConflictPosition(state.position, pos) {
			c.forgetPointEvidence(uid, state.position, state.reason)
			c.Discard(uid, state.position)
			c.Discard(uid, pos)
			*state = AttemptFailureState{}
			return true
		}
		c.Discard(uid, state.position)
		*state = AttemptFailureState{}
	}
	if ambiguousPointFailureReason(reason) {
		c.rememberPointEvidence(uid, pos, reason)
		state.position = pos
		state.reason = reason
		return false
	}
	c.Report(uid, pos, false, reason)
	return false
}

func (c *PointCoordinator) CommitAttemptFailure(uid int, state *AttemptFailureState) {
	if state == nil || state.reason == "" {
		return
	}
	// The observation remains available to the current process as evidence, but
	// an ambiguous coordinate failure is not strong enough to persist as fact.
	c.Discard(uid, state.position)
	*state = AttemptFailureState{}
}

func (c *PointCoordinator) DiscardAttemptFailure(uid int, state *AttemptFailureState) {
	if state == nil || state.reason == "" {
		return
	}
	c.forgetPointEvidence(uid, state.position, state.reason)
	c.Discard(uid, state.position)
	*state = AttemptFailureState{}
}

func (c *PointCoordinator) rememberPointEvidence(uid int, pos Position, reason string) {
	if uid <= 0 || pos.PointID == "" || !ambiguousPointFailureReason(reason) {
		return
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	now := time.Now()
	c.prunePointEvidenceLocked(now)
	key := pointEvidenceKey{PointID: pos.PointID, Reason: reason}
	byUID := c.pointEvidence[key]
	if byUID == nil {
		byUID = make(map[int]time.Time)
		c.pointEvidence[key] = byUID
	}
	byUID[uid] = now
	if len(byUID) < pointEvidenceLimit {
		return
	}
	lease := pointClaimTTL
	if claim, ok := c.pointClaims[pos.PointID]; ok && claim.UID == uid {
		lease = claim.Lease
	}
	until := now.Add(normalizePointLease(lease))
	if current, ok := c.pointCooldown[pos.PointID]; ok && !current.Before(until) {
		return
	}
	c.pointCooldown[pos.PointID] = until
	c.logf("[StorePoint] evidence_cooldown point=%s reason=%s uids=%d until=%s\n", pos.PointID, reason, len(byUID), until.Format(time.RFC3339))
}

func (c *PointCoordinator) forgetPointEvidence(uid int, pos Position, reason string) {
	if uid <= 0 || pos.PointID == "" || !ambiguousPointFailureReason(reason) {
		return
	}
	c.pointMu.Lock()
	defer c.pointMu.Unlock()
	now := time.Now()
	c.prunePointEvidenceLocked(now)
	key := pointEvidenceKey{PointID: pos.PointID, Reason: reason}
	if byUID := c.pointEvidence[key]; byUID != nil {
		delete(byUID, uid)
		if len(byUID) == 0 {
			delete(c.pointEvidence, key)
		}
	}
	if !c.pointHasEnoughEvidenceLocked(pos.PointID) {
		delete(c.pointCooldown, pos.PointID)
	}
}

func (c *PointCoordinator) pointCoolingDownLocked(pointID string, now time.Time) bool {
	until, ok := c.pointCooldown[pointID]
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(c.pointCooldown, pointID)
	return false
}

func (c *PointCoordinator) pointHasEnoughEvidenceLocked(pointID string) bool {
	for key, byUID := range c.pointEvidence {
		if key.PointID == pointID && len(byUID) >= pointEvidenceLimit {
			return true
		}
	}
	return false
}

func (c *PointCoordinator) prunePointEvidenceLocked(now time.Time) {
	cutoff := now.Add(-pointEvidenceWindow)
	for key, byUID := range c.pointEvidence {
		for uid, observedAt := range byUID {
			if observedAt.Before(cutoff) {
				delete(byUID, uid)
			}
		}
		if len(byUID) == 0 {
			delete(c.pointEvidence, key)
		}
	}
}

func (c *PointCoordinator) clearPointEvidenceLocked(pointID string) {
	for key := range c.pointEvidence {
		if key.PointID == pointID {
			delete(c.pointEvidence, key)
		}
	}
}
