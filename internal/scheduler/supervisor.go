package scheduler

import (
	"sort"
	"sync"
	"time"

	actormodel "robot/internal/actor"
	"robot/internal/capability/keypair"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

type RobotSupervisor struct {
	manager *RobotManager
	runtime actormodel.RobotRuntime

	ledger actormodel.Ledger

	stop chan struct{}
	done chan struct{}
	once sync.Once

	nextMetrics      time.Time
	nextKeyLog       time.Time
	nextLeaseHealth  time.Time
	nextAnnouncement time.Time
}

func NewRobotSupervisor(manager *RobotManager, runtime actormodel.RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager: manager,
		runtime: runtime,
		ledger:  actormodel.NewLedger(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *RobotSupervisor) Start() {
	go s.loop()
}

func (s *RobotSupervisor) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

const actorStopConcurrency = 60

func (s *RobotSupervisor) stopAutoActors(logout bool) {
	stopActorsConcurrent(s.ledger.DetachAutoActors(), logout)
}

func (s *RobotSupervisor) stopSomeAutoActors(logout bool, limit, floor int) {
	if limit <= 0 {
		return
	}
	status := s.manager.runtimeStatusMap()
	actors := s.ledger.DetachSomeAutoActors(status, limit, floor)
	if len(actors) == 0 {
		return
	}
	robotLogf("[RobotSupervisor] pressure_release actors=%d floor=%d logout=%v\n", len(actors), floor, logout)
	go stopActorsConcurrent(actors, logout)
}

func (s *RobotSupervisor) stopAll(logout bool) {
	stopActorsConcurrent(s.ledger.DetachAllActors(), logout)
}

func stopActorsConcurrent(actors []*actormodel.Actor, logout bool) {
	if len(actors) == 0 {
		return
	}
	workers := actorStopConcurrency
	if len(actors) < workers {
		workers = len(actors)
	}
	jobs := make(chan *actormodel.Actor)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for actor := range jobs {
				if logout {
					actor.ReleaseAndWait(10 * time.Second)
				}
				actor.StopAndWait(5 * time.Second)
			}
		}()
	}
	for _, actor := range actors {
		jobs <- actor
	}
	close(jobs)
	wg.Wait()
}

func (s *RobotSupervisor) assignIdleAutoActors(rc robotconfig.RuntimeConfig) {
	idle := s.idleAutoActors()
	if len(idle) == 0 {
		return
	}
	sort.Slice(idle, func(i, j int) bool {
		return idle[i].SlotIDValue() < idle[j].SlotIDValue()
	})
	limit := robotconfig.OnlineStartRateForNeed(len(idle), rc)
	if limit > len(idle) {
		limit = len(idle)
	}
	pairs := s.acquireUIDs(rc, idle[:limit])
	for _, pair := range pairs {
		if pair.actor.AssignAndWait(pair.uid, 10*time.Second) {
			continue
		}
		s.ledger.UnleaseUID(pair.uid, pair.actor)
		robotLogf("[RobotSupervisor] assign_failed slot=%d uid=%d\n", pair.actor.SlotIDValue(), pair.uid)
	}
}

func (s *RobotSupervisor) idleAutoActors() []*actormodel.Actor {
	return s.ledger.IdleAutoActors()
}

type actorLease struct {
	actor *actormodel.Actor
	uid   int
}

func (s *RobotSupervisor) acquireUIDs(rc robotconfig.RuntimeConfig, actors []*actormodel.Actor) []actorLease {
	if len(actors) == 0 {
		return nil
	}
	robots, err := s.manager.repo().SelectRobots(robotcap.CommandRequest{Count: rc.MaxOnlineRobots})
	if err != nil {
		robotLogf("[RobotSupervisor] select_robots_failed err=%v\n", err)
		return nil
	}
	out := make([]actorLease, 0, len(actors))
	nextActor := 0
	for _, robot := range robots {
		if nextActor >= len(actors) {
			return out
		}
		actor := actors[nextActor]
		if s.ledger.TryLeaseUID(robot.UID, actor) {
			out = append(out, actorLease{actor: actor, uid: robot.UID})
			nextActor++
		}
	}
	need := len(actors) - nextActor
	if need <= 0 {
		return out
	}
	target := robotconfig.TargetCapacity(rc)
	createRoom := robotconfig.CreateRoom(rc, len(robots))
	if createRoom <= 0 {
		robotLogf("[RobotSupervisor] create_blocked_by_target existing=%d target=%d need=%d blocked=%d\n", len(robots), target, need, s.ledger.BlockedCount())
		return out
	}
	if need > createRoom {
		need = createRoom
	}
	created, err := s.manager.CreateRobots(robotcap.CreateRequest{Count: need})
	if err != nil {
		robotLogf("[RobotSupervisor] create_failed count=%d err=%v\n", need, err)
		return out
	}
	if len(created) > 0 {
		s.manager.addAutoCreated(len(created))
	}
	for _, robot := range created {
		if nextActor >= len(actors) {
			break
		}
		actor := actors[nextActor]
		if s.ledger.TryLeaseUID(robot.UID, actor) {
			out = append(out, actorLease{actor: actor, uid: robot.UID})
			nextActor++
		}
	}
	return out
}

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
	s.sendSystemAnnouncementIfDue(now)
	if s.handleAutoGuards(now, rc) {
		return
	}
	s.maintainTarget(rc)
	s.releaseBrokenLeases(now, rc)
	s.cleanupBlockedUIDs(10)
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc)
}

// Auto scheduling guards.

func (s *RobotSupervisor) handleAutoGuards(now time.Time, rc robotconfig.RuntimeConfig) bool {
	s.manager.autoMu.Lock()
	enabled := s.manager.autoEnabled
	s.manager.autoMu.Unlock()
	if !enabled || !rc.AutoActions {
		s.updateGuardStatus(rc, schedulerPolicyManual, schedulerReasonAutoDisabled)
		s.updateMetrics(rc)
		return true
	}
	if st := s.manager.KeypairStatus(); !st.GameValid {
		s.stopAutoActors(true)
		s.logKeyBlocked(now, rc, st)
		reason := st.Error
		if reason == "" {
			reason = st.KeyReason
		}
		if reason == "" {
			reason = schedulerReasonKeyInvalid
		}
		s.updateGuardStatus(rc, schedulerPolicyMaintenance, schedulerReasonKeyInvalidPrefix+reason)
		s.updateMetrics(rc)
		return true
	}
	if op, started, active := s.manager.structuralOperation(); active {
		s.manager.updateSchedulerStatus(rc, s.manager.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: schedulerReasonStructuralPrefix + op})
		s.updateMetrics(rc)
		robotLogf("[RobotSupervisor] paused structural_op=%s started=%s\n", op, started.Format(time.RFC3339))
		return true
	}
	if !s.manager.autoGamePortStable(now, rc) {
		s.stopSomeAutoActors(true, rc.SchedulerPortDownReleaseBatch, 0)
		s.updateGuardStatus(rc, schedulerPolicyPressure, schedulerReasonGamePortUnstable)
		s.updateMetrics(rc)
		return true
	}
	if s.manager.autoBreakerActive(now) {
		s.recycleUnhealthyActors(now, rc)
		s.stopSomeAutoActors(true, rc.SchedulerBreakerReleaseBatch, robotconfig.BreakerActorFloor(rc))
		s.updateGuardStatus(rc, schedulerPolicyBreaker, schedulerReasonBreakerActive)
		s.updateMetrics(rc)
		return true
	}
	return false
}

func (s *RobotSupervisor) updateGuardStatus(rc robotconfig.RuntimeConfig, mode schedulerPolicyMode, reason string) {
	s.manager.updateSchedulerStatus(rc, s.manager.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: mode, Reason: reason})
}

// Auto scheduling policy and actor scaling.

func (s *RobotSupervisor) logKeyBlocked(now time.Time, rc robotconfig.RuntimeConfig, st keypair.KeypairStatus) {
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
	robotLogf("[RobotSupervisor] auto_blocked key_state=%s reason=%s\n", st.KeyState, reason)
}

func (s *RobotSupervisor) maintainTarget(rc robotconfig.RuntimeConfig) {
	if err := s.manager.repo().EnsureSchema(); err != nil {
		robotLogf("[RobotSupervisor] ensure_schema_failed err=%v\n", err)
		return
	}
	s.ensureAutoActorSlots(rc, robotconfig.TargetCapacity(rc))
}

func (s *RobotSupervisor) ensureAutoActorSlots(rc robotconfig.RuntimeConfig, target int) {
	status := s.manager.runtimeStatusMap()
	extra := s.ledger.EnsureAutoActorSlots(s.runtime, rc, target, status)
	stopActorsConcurrent(extra, true)
}

// Lease health, broken UID cleanup, and recycle paths.

func (s *RobotSupervisor) releaseBrokenLeases(now time.Time, rc robotconfig.RuntimeConfig) {
	if !s.nextLeaseHealth.IsZero() && now.Before(s.nextLeaseHealth) {
		return
	}
	interval := time.Duration(rc.SchedulerMetricsIntervalSec) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	s.nextLeaseHealth = now.Add(interval)

	leases := s.ledger.LeaseSnapshots()
	if len(leases) == 0 {
		return
	}
	uids := make([]int, 0, len(leases))
	for _, item := range leases {
		uids = append(uids, item.UID)
	}
	alive, err := s.manager.aliveRobotUIDs(uids)
	if err != nil {
		robotLogf("[RobotSupervisor] lease_health_check_failed err=%v\n", err)
		return
	}
	for _, item := range leases {
		if alive[item.UID] {
			continue
		}
		if !s.ledger.BlockLeaseIfCurrent(item.UID, item.Actor) {
			continue
		}
		robotLogf("[RobotSupervisor] broken_lease uid=%d slot=%d action=release_cleanup\n", item.UID, item.Actor.SlotIDValue())
		released := item.Actor.ReleaseAndWait(10 * time.Second)
		if released != item.UID && released > 0 {
			s.ledger.UnleaseUID(released, item.Actor)
		}
		if released == item.UID || !s.actorOwnsUID(item.UID) {
			s.cleanupBrokenUID(item.UID)
		}
	}
}

func (s *RobotSupervisor) cleanupBrokenUID(uid int) {
	if uid <= 0 {
		return
	}
	result, err := s.manager.CleanupRobots(robotcap.CleanupRequest{UIDs: []int{uid}, Force: true, InternalConfirmedBroken: true})
	if err != nil {
		robotLogf("[RobotSupervisor] broken_cleanup_failed uid=%d err=%v\n", uid, err)
		return
	}
	if result.Deleted <= 0 {
		robotLogf("[RobotSupervisor] broken_cleanup_skipped uid=%d requested=%d skipped=%d\n", uid, result.Requested, result.Skipped)
		return
	}
	s.ledger.UnblockUID(uid)
	robotLogf("[RobotSupervisor] broken_cleanup_done uid=%d deleted=%d skipped=%d\n", uid, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) cleanupBlockedUIDs(limit int) {
	for _, uid := range s.ledger.BlockedUIDs(limit) {
		if s.actorOwnsUID(uid) {
			robotLogf("[RobotSupervisor] blocked_cleanup_deferred uid=%d reason=actor_still_attached\n", uid)
			continue
		}
		s.cleanupBrokenUID(uid)
	}
}

func (s *RobotSupervisor) actorOwnsUID(uid int) bool {
	return s.ledger.ActorOwnsUID(uid)
}

func (m *RobotManager) aliveRobotUIDs(uids []int) (map[int]bool, error) {
	return m.schemaRepo().AliveRobotUIDs(uids)
}

func (s *RobotSupervisor) recycleActorUID(actor *actormodel.Actor, status actormodel.Status) {
	if status.UID <= 0 {
		return
	}
	robotLogf("[RobotSupervisor] recycle_uid slot=%d uid=%d health=%s reason=%s failures=%d first_failure=%s state=%s\n",
		status.SlotID, status.UID, status.Health, status.HealthReason, status.Failures, status.FirstFailureAt.Format(time.RFC3339), status.State)
	released := actor.ReleaseAndWait(15 * time.Second)
	if released <= 0 {
		released = status.UID
	}
	s.ledger.RemoveLeaseIfActor(released, actor)
	result, err := s.manager.CleanupRobots(robotcap.CleanupRequest{UIDs: []int{released}, Force: true, InternalConfirmedBroken: true})
	if err != nil {
		robotLogf("[RobotSupervisor] recycle_cleanup_failed uid=%d err=%v\n", released, err)
		s.ledger.BlockUID(released)
		return
	}
	robotLogf("[RobotSupervisor] recycle_cleanup_done uid=%d deleted=%d skipped=%d\n", released, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) recycleUnhealthyActors(now time.Time, rc robotconfig.RuntimeConfig) {
	type recycleCandidate struct {
		actor  *actormodel.Actor
		status actormodel.Status
	}
	var unhealthy []recycleCandidate
	for _, actor := range s.ledger.AutoActorPointers() {
		status := actor.Status(now, rc)
		if status.RecycleUID {
			unhealthy = append(unhealthy, recycleCandidate{actor: actor, status: status})
		}
	}
	for _, item := range unhealthy {
		s.recycleActorUID(item.actor, item.status)
	}
}
