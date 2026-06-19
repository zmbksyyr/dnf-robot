package service

import (
	"fmt"
	"sort"
	"time"

	"robot/internal/dnf"
)

// Automatic scheduler loop.

func (s *RobotSupervisor) loop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			s.stopAll(true)
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *RobotSupervisor) tick(now time.Time) {
	rc := s.manager.loadRobotConfig()
	s.manager.autoMu.Lock()
	enabled := s.manager.autoEnabled
	s.manager.autoMu.Unlock()
	if !enabled || !rc.AutoActions {
		s.updateMetrics(rc)
		return
	}
	if st := s.manager.KeypairStatus(); !st.GameValid {
		s.stopAutoActors(true)
		s.logKeyBlocked(now, rc, st)
		s.updateMetrics(rc)
		return
	}
	if op, started, active := s.manager.structuralOperation(); active {
		s.manager.updateSchedulerStatus(rc, s.manager.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: "structural_op=" + op})
		s.updateMetrics(rc)
		robotLogf("[RobotSupervisor] paused structural_op=%s started=%s\n", op, started.Format(time.RFC3339))
		return
	}
	if !s.manager.autoGamePortStable(now, rc) {
		s.stopSomeAutoActors(true, rc.SchedulerPortDownReleaseBatch, 0)
		s.updateMetrics(rc)
		return
	}
	if s.manager.autoBreakerActive(now) {
		s.recycleUnhealthyActors(now, rc)
		s.stopSomeAutoActors(true, rc.SchedulerBreakerReleaseBatch, breakerActorFloor(rc))
		s.updateMetrics(rc)
		return
	}
	s.maintainTarget(rc)
	s.releaseBrokenLeases()
	s.cleanupBlockedUIDs(10)
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc)
}

// Auto scheduling policy and actor scaling.

func (s *RobotSupervisor) logKeyBlocked(now time.Time, rc robotRuntimeConfig, st KeypairStatus) {
	if !s.nextKeyLog.IsZero() && now.Before(s.nextKeyLog) {
		return
	}
	interval := time.Duration(rc.SchedulerMetricsIntervalSec) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	s.nextKeyLog = now.Add(interval)
	reason := st.Error
	if reason == "" {
		reason = st.KeyReason
	}
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("[RobotSupervisor] auto_blocked key_state=%s reason=%s\n", st.KeyState, reason))
}

func (s *RobotSupervisor) maintainTarget(rc robotRuntimeConfig) {
	if err := s.manager.ensureSchema(); err != nil {
		robotLogf("[RobotSupervisor] ensure_schema_failed err=%v\n", err)
		return
	}
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	s.ensureAutoActorSlots(rc, target)
}

func (s *RobotSupervisor) ensureAutoActorSlots(rc robotRuntimeConfig, target int) {
	if target < 0 {
		target = 0
	}
	status := s.manager.runtimeStatusMap()
	s.mu.Lock()
	current := 0
	pending := 0
	for _, actor := range s.actors {
		if actor.modeValue() == robotActorAuto {
			current++
			snap := actor.snapshot()
			if snap.schedulerPending() {
				pending++
			}
		}
	}
	addCount := target - current
	if addCount < 0 {
		addCount = 0
	}
	pendingLimit := schedulerPendingActorLimit(target, rc)
	if pending >= pendingLimit {
		addCount = 0
	}
	if addCount > schedulerScaleUpBatch(rc) {
		addCount = schedulerScaleUpBatch(rc)
	}
	added := addCount
	for addCount > 0 {
		slotID := s.nextSlotLocked()
		actor := newRobotActor(slotID, robotActorAuto, s.runtime)
		s.actors[slotID] = actor
		actor.start()
		addCount--
	}
	current += added
	if current <= target {
		s.mu.Unlock()
		return
	}
	var candidates []*robotActor
	for _, actor := range s.actors {
		if actor.modeValue() != robotActorAuto {
			continue
		}
		candidates = append(candidates, actor)
	}
	sortActorsForStopByPolicy(candidates, status)
	removeCount := current - target
	if removeCount > schedulerScaleDownBatch(current, target) {
		removeCount = schedulerScaleDownBatch(current, target)
	}
	if removeCount > len(candidates) {
		removeCount = len(candidates)
	}
	extra := candidates[:removeCount]
	for _, actor := range extra {
		delete(s.actors, actor.slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(s.uidActors, uid)
		}
	}
	s.mu.Unlock()
	stopActorsConcurrent(extra, true)
}

func schedulerScaleUpBatch(rc robotRuntimeConfig) int {
	batch := rc.SchedulerOnlineBatchSize
	if batch < 0 {
		return 0
	}
	if batch <= 0 {
		batch = 20
	}
	if batch > 120 {
		batch = 120
	}
	return batch
}

func schedulerPendingActorLimit(target int, rc robotRuntimeConfig) int {
	if target <= 0 {
		return 1
	}
	limit := schedulerOnlineStartRate(rc) * 8
	if limit < target/10 {
		limit = target / 10
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 120 {
		limit = 120
	}
	return limit
}

func schedulerScaleDownBatch(current, target int) int {
	delta := current - target
	if delta <= 0 {
		return 0
	}
	batch := current / 25
	if current%25 != 0 {
		batch++
	}
	if batch < 5 {
		batch = 5
	}
	if batch > 50 {
		batch = 50
	}
	if batch > delta {
		batch = delta
	}
	return batch
}

func sortActorsForStopByPolicy(actors []*robotActor, status map[int]RuntimeRobotStatus) {
	sort.Slice(actors, func(i, j int) bool {
		leftPriority := actorStopPriority(actors[i], status)
		rightPriority := actorStopPriority(actors[j], status)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftUID := actors[i].uidValue()
		rightUID := actors[j].uidValue()
		if leftUID <= 0 || rightUID <= 0 {
			if leftUID != rightUID {
				return leftUID <= 0
			}
			return actors[i].slotID > actors[j].slotID
		}
		if leftUID != rightUID {
			return leftUID > rightUID
		}
		return actors[i].slotID > actors[j].slotID
	})
}

func actorStopPriority(actor *robotActor, status map[int]RuntimeRobotStatus) int {
	uid := actor.uidValue()
	if uid <= 0 {
		return 0
	}
	st, ok := status[uid]
	if !ok || st.DisconnectReason != 0 || st.StateName == "init" || st.StateName == "login" {
		return 1
	}
	if st.RobotType == 2 || st.RobotType == 3 || st.StoreDisplayAck {
		return 2
	}
	return 3
}

// Lease health, broken UID cleanup, and recycle paths.

func (s *RobotSupervisor) recycleUnhealthyActors(now time.Time, rc robotRuntimeConfig) {
	type recycleCandidate struct {
		actor  *robotActor
		status robotActorStatus
	}
	var unhealthy []recycleCandidate
	s.mu.Lock()
	for _, actor := range s.actors {
		status := actor.status(now, rc)
		if status.RecycleUID {
			unhealthy = append(unhealthy, recycleCandidate{actor: actor, status: status})
		}
	}
	s.mu.Unlock()
	for _, item := range unhealthy {
		s.recycleActorUID(item.actor, item.status)
	}
}

func (s *RobotSupervisor) nextSlotLocked() int {
	s.nextSlotID++
	return s.nextSlotID
}

func breakerActorFloor(rc robotRuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	floorPct := rc.SchedulerBreakerFloorPct
	if floorPct < 0 {
		floorPct = 0
	}
	if floorPct > 100 {
		floorPct = 100
	}
	floor := target * floorPct / 100
	if target <= 50 && floor < target {
		return target
	}
	return floor
}
