package service

import (
	"fmt"
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
	if s.handleAutoGuards(now, rc) {
		return
	}
	s.maintainTarget(rc)
	s.releaseBrokenLeases()
	s.cleanupBlockedUIDs(10)
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc)
}

// Auto scheduling guards.

func (s *RobotSupervisor) handleAutoGuards(now time.Time, rc robotRuntimeConfig) bool {
	s.manager.autoMu.Lock()
	enabled := s.manager.autoEnabled
	s.manager.autoMu.Unlock()
	if !enabled || !rc.AutoActions {
		s.updateMetrics(rc)
		return true
	}
	if st := s.manager.KeypairStatus(); !st.GameValid {
		s.stopAutoActors(true)
		s.logKeyBlocked(now, rc, st)
		s.updateMetrics(rc)
		return true
	}
	if op, started, active := s.manager.structuralOperation(); active {
		s.manager.updateSchedulerStatus(rc, s.manager.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: "structural_op=" + op})
		s.updateMetrics(rc)
		robotLogf("[RobotSupervisor] paused structural_op=%s started=%s\n", op, started.Format(time.RFC3339))
		return true
	}
	if !s.manager.autoGamePortStable(now, rc) {
		s.stopSomeAutoActors(true, rc.SchedulerPortDownReleaseBatch, 0)
		s.updateMetrics(rc)
		return true
	}
	if s.manager.autoBreakerActive(now) {
		s.recycleUnhealthyActors(now, rc)
		s.stopSomeAutoActors(true, rc.SchedulerBreakerReleaseBatch, breakerActorFloor(rc))
		s.updateMetrics(rc)
		return true
	}
	return false
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
	s.ensureAutoActorSlots(rc, schedulerTargetCapacity(rc))
}

func (s *RobotSupervisor) ensureAutoActorSlots(rc robotRuntimeConfig, target int) {
	status := s.manager.runtimeStatusMap()
	extra := s.ledger.ensureAutoActorSlots(s.runtime, rc, target, status)
	stopActorsConcurrent(extra, true)
}

// Lease health, broken UID cleanup, and recycle paths.

func (s *RobotSupervisor) recycleUnhealthyActors(now time.Time, rc robotRuntimeConfig) {
	type recycleCandidate struct {
		actor  *robotActor
		status robotActorStatus
	}
	var unhealthy []recycleCandidate
	for _, actor := range s.ledger.autoActorPointers() {
		status := actor.status(now, rc)
		if status.RecycleUID {
			unhealthy = append(unhealthy, recycleCandidate{actor: actor, status: status})
		}
	}
	for _, item := range unhealthy {
		s.recycleActorUID(item.actor, item.status)
	}
}
