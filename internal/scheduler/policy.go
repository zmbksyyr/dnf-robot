package scheduler

import (
	"errors"
	"fmt"
	"net"
	actormodel "robot/internal/actor"
	"robot/internal/capability/keypair"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/process"
	"sort"
	"strconv"
	"sync"
	"time"
)

func (m *RobotManager) StartAutoActions() {
	m.autoMu.Lock()
	if m.supervisor != nil {
		m.autoMu.Unlock()
		return
	}
	runtime := NewRobotRuntime(m)
	supervisor := NewRobotSupervisor(m, runtime)
	m.supervisor = supervisor
	m.autoStoreBusy = make(map[int]bool)
	m.autoPortSince = time.Time{}
	m.autoPortReady = false
	m.autoPortLog = time.Time{}
	m.autoEnabled = true
	m.autoMu.Unlock()
	supervisor.Start()
}

func (m *RobotManager) StopAutoActions() {
	m.autoMu.Lock()
	supervisor := m.supervisor
	m.supervisor = nil
	m.autoEnabled = false
	m.autoMu.Unlock()
	if supervisor != nil {
		supervisor.Stop()
	}
}

func (m *RobotManager) SetAutoEnabled(enabled bool) robotcap.AutoStatus {
	_ = m.writeRobotConfigValues(map[string]string{
		"auto.auto_actions": strconv.FormatBool(enabled),
	})
	m.autoMu.Lock()
	supervisor := m.supervisor
	m.autoEnabled = enabled
	m.autoMu.Unlock()
	if !enabled && supervisor != nil {
		end := m.beginActorContainerOp("auto_stop")
		defer end()
		rc := m.loadRobotConfig()
		supervisor.stopAutoActors(true)
		summary := robotcap.SummarizeRuntimeStatusMap(m.runtimeStatusMap())
		running, connecting, stores := summary.Running, summary.Connecting, summary.Stores
		m.updateAutoSnapshot(rc, running, connecting, stores)
		m.updateAutoActorSnapshot(supervisor.actorCounts(time.Now(), rc))
		m.updateSchedulerStatus(rc, m.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: schedulerPolicyManual, Reason: "auto_disabled"})
	}
	return m.AutoStatus()
}

func (m *RobotManager) AutoStatus() robotcap.AutoStatus {
	rc := m.loadRobotConfig()
	status := m.doll.RuntimeStatus()
	summary := robotcap.SummarizeRuntimeStatusSlice(status)
	running, connecting, stores := summary.Running, summary.Connecting, summary.Stores
	m.autoMu.Lock()
	out := m.autoStats
	out.Enabled = m.autoEnabled && rc.AutoActions
	m.autoMu.Unlock()
	out.TargetOnline = rc.AutoTargetOnlineCount
	out.Running = running
	out.Connecting = connecting
	if out.GamePortAddress == "" {
		out.GamePortAddress = m.robotGamePortAddress()
	}
	out.StoreProbability = rc.AutoStoreProbabilityPercent
	out.StoreRunning = stores
	out.UpdatedAt = time.Now()
	return out
}

func (m *RobotManager) autoActionsEnabled(rc robotconfig.RuntimeConfig) bool {
	m.autoMu.Lock()
	enabled := m.autoEnabled
	m.autoMu.Unlock()
	return enabled && rc.AutoActions
}

func (m *RobotManager) autoGamePortStable(now time.Time, rc robotconfig.RuntimeConfig) bool {
	addr := m.robotGamePortAddress()
	timeout := time.Duration(rc.AutoGamePortCheckTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	open := err == nil
	if conn != nil {
		_ = conn.Close()
	}

	stableFor := time.Duration(rc.AutoGamePortStableSec) * time.Second
	if stableFor <= 0 {
		stableFor = 15 * time.Second
	}

	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	m.autoStats.GamePortAddress = addr
	if !open {
		if m.autoPortReady || now.Sub(m.autoPortLog) >= 10*time.Second {
			robotLogf("[AutoGate] game_port_not_ready addr=%s err=%v\n", addr, err)
			m.autoPortLog = now
		}
		m.autoPortSince = time.Time{}
		m.autoPortReady = false
		m.autoStats.GamePortReady = false
		m.autoStats.GamePortStableAt = time.Time{}
		m.autoStats.UpdatedAt = now
		return false
	}
	if m.autoPortSince.IsZero() {
		m.autoPortSince = now
	}
	stableAt := m.autoPortSince.Add(stableFor)
	m.autoStats.GamePortStableAt = stableAt
	if now.Before(stableAt) {
		if now.Sub(m.autoPortLog) >= 10*time.Second {
			robotLogf("[AutoGate] game_port_wait_stable addr=%s stable_at=%s\n", addr, stableAt.Format(time.RFC3339))
			m.autoPortLog = now
		}
		m.autoPortReady = false
		m.autoStats.GamePortReady = false
		m.autoStats.UpdatedAt = now
		return false
	}
	if !m.autoPortReady {
		robotLogf("[AutoGate] game_port_stable addr=%s stable_for=%s\n", addr, stableFor)
	}
	m.autoPortReady = true
	m.autoStats.GamePortReady = true
	m.autoStats.UpdatedAt = now
	return true
}

const runtimeStatusCacheTTL = 2000 * time.Millisecond

func (m *RobotManager) runtimeStatusMap() map[int]robotcap.RuntimeStatus {
	now := time.Now()
	var out map[int]robotcap.RuntimeStatus
	_ = m.lockHub().WithResource(lockScopeScheduler, lockResourceSchedulerRuntimeStatus, "runtime_status_cache_read", func() error {
		if !m.runtimeStatusCacheAt.IsZero() && now.Sub(m.runtimeStatusCacheAt) <= runtimeStatusCacheTTL && m.runtimeStatusCache != nil {
			out = robotcap.CopyRuntimeStatusMap(m.runtimeStatusCache)
		}
		return nil
	})
	if out != nil {
		return out
	}

	status := map[int]robotcap.RuntimeStatus{}
	for _, st := range m.doll.RuntimeStatus() {
		status[st.UID] = st
	}
	_ = m.lockHub().WithResource(lockScopeScheduler, lockResourceSchedulerRuntimeStatus, "runtime_status_cache_write", func() error {
		m.runtimeStatusCache = robotcap.CopyRuntimeStatusMap(status)
		m.runtimeStatusCacheAt = now
		return nil
	})
	return status
}

func (m *RobotManager) countRuntimeRunning() int {
	n := 0
	for _, st := range m.doll.RuntimeStatus() {
		if robotcap.ActiveRuntimeStatus(st) {
			n++
		}
	}
	return n
}

func (m *RobotManager) addAutoCreated(n int) {
	if n <= 0 {
		return
	}
	m.autoMu.Lock()
	m.autoStats.Created += n
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoOnline(success, failed int) {
	m.autoMu.Lock()
	m.autoStats.OnlineSuccess += success
	m.autoStats.OnlineFailed += failed
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoMove(success, failed int) {
	m.autoMu.Lock()
	m.autoStats.MoveSuccess += success
	m.autoStats.MoveFailed += failed
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoShoutChannel(world bool, success, failed int) {
	m.autoMu.Lock()
	if world {
		m.autoStats.ShoutWorldSuccess += success
		m.autoStats.ShoutWorldFailed += failed
	} else {
		m.autoStats.ShoutLocalSuccess += success
		m.autoStats.ShoutLocalFailed += failed
	}
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoStore(success, failed, expired int) {
	m.autoMu.Lock()
	m.autoStats.StoreSuccess += success
	m.autoStats.StoreFailed += failed
	m.autoStats.StoreExpired += expired
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoSnapshot(rc robotconfig.RuntimeConfig, running, connecting, storeRunning int) {
	m.autoMu.Lock()
	m.autoStats.Enabled = m.autoEnabled && rc.AutoActions
	m.autoStats.TargetOnline = rc.AutoTargetOnlineCount
	m.autoStats.Running = running
	m.autoStats.Connecting = connecting
	m.autoStats.StoreProbability = rc.AutoStoreProbabilityPercent
	m.autoStats.StoreRunning = storeRunning
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoActorSnapshot(counts actormodel.LedgerCounts) {
	m.autoMu.Lock()
	m.autoStats.Actors = counts.Auto
	m.autoStats.Leased = counts.Leased
	m.autoStats.Idle = counts.Idle
	m.autoStats.Recycling = counts.Releasing
	m.autoStats.BlockedUIDs = counts.Blocked
	m.autoStats.ActorIdle = counts.StateIdle
	m.autoStats.ActorAssigned = counts.StateAssigned
	m.autoStats.ActorOnline = counts.StateOnline
	m.autoStats.ActorRunning = counts.StateRunning
	m.autoStats.ActorBusy = counts.StateBusy
	m.autoStats.ActorReleasing = counts.StateReleasing
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoBreaker(now time.Time, rc robotconfig.RuntimeConfig, counts actormodel.LedgerCounts, running, connecting int) {
	target := rc.AutoTargetOnlineCount
	if target <= 0 {
		return
	}
	if target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	abnormalPct := rc.SchedulerBreakerAbnormalPct
	if abnormalPct <= 0 {
		abnormalPct = 30
	}
	if abnormalPct > 100 {
		abnormalPct = 100
	}
	threshold := (target*abnormalPct + 99) / 100
	readyForBreaker := counts.Auto >= (target*9+9)/10 || counts.Leased >= (target*9+9)/10

	m.autoMu.Lock()
	defer m.autoMu.Unlock()

	stats := m.autoStats
	reason := ""
	if readyForBreaker && connecting >= threshold {
		reason = fmt.Sprintf("connecting_over_%dpct target=%d connecting=%d", abnormalPct, target, connecting)
	}

	if m.autoBreakerLastCheck.IsZero() || now.Sub(m.autoBreakerLastCheck) >= time.Minute {
		failDelta := (stats.OnlineFailed - m.autoBreakerLastOnlineFailed) +
			(stats.MoveFailed - m.autoBreakerLastMoveFailed) +
			(stats.ShoutLocalFailed - m.autoBreakerLastShoutLocalFailed) +
			(stats.ShoutWorldFailed - m.autoBreakerLastShoutWorldFailed) +
			(stats.StoreFailed - m.autoBreakerLastStoreFailed)
		m.autoBreakerLastCheck = now
		m.autoBreakerLastOnlineFailed = stats.OnlineFailed
		m.autoBreakerLastMoveFailed = stats.MoveFailed
		m.autoBreakerLastShoutLocalFailed = stats.ShoutLocalFailed
		m.autoBreakerLastShoutWorldFailed = stats.ShoutWorldFailed
		m.autoBreakerLastStoreFailed = stats.StoreFailed
		if readyForBreaker && failDelta >= threshold {
			reason = fmt.Sprintf("failures_over_%dpct_per_min target=%d failed_delta=%d", abnormalPct, target, failDelta)
		}
	}

	if reason == "" {
		return
	}
	pauseSec := rc.SchedulerBreakerPauseSec
	if pauseSec <= 0 {
		pauseSec = 300
	}
	until := now.Add(time.Duration(pauseSec) * time.Second)
	wasActive := now.Before(m.autoBreakerUntil)
	if until.After(m.autoBreakerUntil) {
		m.autoBreakerUntil = until
	}
	if !wasActive || m.autoBreakerReason != reason {
		m.autoBreakerReason = reason
		robotLogf("[AutoBreaker] pause_until=%s reason=%s running=%d connecting=%d actors=%d leased=%d failed online=%d move=%d local=%d world=%d store=%d\n",
			m.autoBreakerUntil.Format(time.RFC3339), reason, running, connecting, counts.Auto, counts.Leased,
			stats.OnlineFailed, stats.MoveFailed, stats.ShoutLocalFailed, stats.ShoutWorldFailed, stats.StoreFailed)
	}
}

func (m *RobotManager) autoBreakerActive(now time.Time) bool {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return now.Before(m.autoBreakerUntil)
}

func (s *RobotSupervisor) updateMetrics(rc robotconfig.RuntimeConfig) {
	now := time.Now()
	if !s.nextMetrics.IsZero() && now.Before(s.nextMetrics) {
		return
	}
	s.nextMetrics = now.Add(time.Duration(rc.SchedulerMetricsIntervalSec) * time.Second)
	status := s.manager.runtimeStatusMap()
	s.filterBlockedRuntimeStatus(status)
	s.filterMissingRuntimeStatus(status)
	summary := robotcap.SummarizeRuntimeStatusMap(status)
	running, connecting, stores := summary.Running, summary.Connecting, summary.Stores
	s.manager.updateAutoSnapshot(rc, running, connecting, stores)
	counts := s.ledger.Counts(now, rc)
	s.manager.updateAutoActorSnapshot(counts)
	s.manager.updateAutoBreaker(now, rc, counts, running, connecting)
	cpu, mem, threads := process.ResourceSnapshot()
	s.manager.autoMu.Lock()
	stats := s.manager.autoStats
	policy := s.manager.schedulerStatus
	s.manager.autoMu.Unlock()
	line := fmt.Sprintf("[RobotMetrics] policy=%s target=%d actors=%d leased=%d idle=%d state idle=%d assigned=%d online=%d running=%d busy=%d releasing=%d runtime running=%d store=%d connecting=%d recycling=%d blocked=%d cpu=%.1f mem_mb=%d goroutines=%d online=%d/%d move=%d/%d shout_local=%d/%d shout_world=%d/%d store=%d/%d expired=%d\n",
		policy.Mode,
		rc.AutoTargetOnlineCount, counts.Auto, counts.Leased, counts.Idle,
		counts.StateIdle, counts.StateAssigned, counts.StateOnline, counts.StateRunning, counts.StateBusy, counts.StateReleasing,
		running, stores, connecting, counts.Releasing, counts.Blocked,
		cpu, mem, threads,
		stats.OnlineSuccess, stats.OnlineFailed,
		stats.MoveSuccess, stats.MoveFailed,
		stats.ShoutLocalSuccess, stats.ShoutLocalFailed,
		stats.ShoutWorldSuccess, stats.ShoutWorldFailed,
		stats.StoreSuccess, stats.StoreFailed, stats.StoreExpired)
	robotLogf("%s", line)
}

func (s *RobotSupervisor) filterBlockedRuntimeStatus(status map[int]robotcap.RuntimeStatus) {
	s.ledger.FilterBlockedRuntimeStatus(status)
}

func (s *RobotSupervisor) filterMissingRuntimeStatus(status map[int]robotcap.RuntimeStatus) {
	if len(status) == 0 {
		return
	}
	uids := make([]int, 0, len(status))
	for uid := range status {
		uids = append(uids, uid)
	}
	alive, err := s.manager.aliveRobotUIDs(uids)
	if err != nil {
		robotLogf("[RobotSupervisor] runtime_alive_filter_failed err=%v\n", err)
		return
	}
	for uid := range status {
		if !alive[uid] {
			delete(status, uid)
		}
	}
}

func (s *RobotSupervisor) actorCounts(now time.Time, rc robotconfig.RuntimeConfig) actormodel.LedgerCounts {
	return s.ledger.Counts(now, rc)
}

// Automatic scheduler loop.

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
		s.updateGuardStatus(rc, schedulerPolicyManual, "auto_disabled")
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
			reason = "key_invalid"
		}
		s.updateGuardStatus(rc, schedulerPolicyMaintenance, "key_invalid="+reason)
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
		s.updateGuardStatus(rc, schedulerPolicyPressure, "game_port_unstable")
		s.updateMetrics(rc)
		return true
	}
	if s.manager.autoBreakerActive(now) {
		s.recycleUnhealthyActors(now, rc)
		s.stopSomeAutoActors(true, rc.SchedulerBreakerReleaseBatch, robotconfig.BreakerActorFloor(rc))
		s.updateGuardStatus(rc, schedulerPolicyBreaker, "breaker_active")
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

type schedulerPolicyMode string

const (
	schedulerPolicyBootstrap   schedulerPolicyMode = robotcap.SchedulerModeBootstrap
	schedulerPolicyFill        schedulerPolicyMode = robotcap.SchedulerModeFill
	schedulerPolicyStable      schedulerPolicyMode = robotcap.SchedulerModeStable
	schedulerPolicyStore       schedulerPolicyMode = robotcap.SchedulerModeStore
	schedulerPolicyPressure    schedulerPolicyMode = robotcap.SchedulerModePressure
	schedulerPolicyBreaker     schedulerPolicyMode = robotcap.SchedulerModeBreaker
	schedulerPolicyMaintenance schedulerPolicyMode = robotcap.SchedulerModeMaintenance
	schedulerPolicyManual      schedulerPolicyMode = robotcap.SchedulerModeManual
)

type adaptiveSchedulerSignals struct {
	Live           bool
	Running        int
	Connecting     int
	StoreRunning   int
	Actors         int
	Idle           int
	ActorIdle      int
	ActorAssigned  int
	ActorOnline    int
	ActorRunning   int
	ActorBusy      int
	ActorReleasing int
	GamePortReady  bool
	BreakerActive  bool
	CPUPercent     float64
	MemoryMB       int
	Goroutines     int
}

type schedulerPolicyDecision struct {
	Mode   schedulerPolicyMode
	Reason string
}

func (m *RobotManager) applyAdaptiveSchedulerConfig(rc *robotconfig.RuntimeConfig) {
	signals := m.adaptiveSchedulerSignals()
	decision := m.policy().ApplyConfig(rc, signals)
	m.updateSchedulerStatus(*rc, signals, decision)
	m.logSchedulerPolicyDecision(decision)
}

func (m *RobotManager) SchedulerStatus() robotcap.SchedulerStatus {
	rc := m.loadRobotConfig()
	signals := m.adaptiveSchedulerSignals()
	decision := m.policy().ApplyConfig(&rc, signals)
	if !m.autoActionsEnabled(rc) {
		decision = schedulerPolicyDecision{Mode: schedulerPolicyManual, Reason: "auto_disabled"}
	}
	m.updateSchedulerStatus(rc, signals, decision)

	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.schedulerStatus
}

func (m *RobotManager) adaptiveSchedulerSignals() adaptiveSchedulerSignals {
	now := time.Now()
	m.autoMu.Lock()
	stats := m.autoStats
	live := !stats.UpdatedAt.IsZero()
	breaker := now.Before(m.autoBreakerUntil)
	m.autoMu.Unlock()

	cpu, mem, threads := process.ResourceSnapshot()
	return adaptiveSchedulerSignals{
		Live:           live,
		Running:        stats.Running,
		Connecting:     stats.Connecting,
		StoreRunning:   stats.StoreRunning,
		Actors:         stats.Actors,
		Idle:           stats.Idle,
		ActorIdle:      stats.ActorIdle,
		ActorAssigned:  stats.ActorAssigned,
		ActorOnline:    stats.ActorOnline,
		ActorRunning:   stats.ActorRunning,
		ActorBusy:      stats.ActorBusy,
		ActorReleasing: stats.ActorReleasing,
		GamePortReady:  !live || stats.GamePortReady,
		BreakerActive:  breaker,
		CPUPercent:     cpu,
		MemoryMB:       mem,
		Goroutines:     threads,
	}
}

func (m *RobotManager) logSchedulerPolicyDecision(decision schedulerPolicyDecision) {
	if decision.Mode == "" {
		return
	}
	m.autoMu.Lock()
	lastMode := m.autoPolicyLastMode
	if lastMode == decision.Mode {
		m.autoMu.Unlock()
		return
	}
	m.autoPolicyLastMode = decision.Mode
	m.autoPolicyLastReason = decision.Reason
	m.autoMu.Unlock()
	robotLogf("[SchedulerPolicy] mode=%s reason=%s\n", decision.Mode, decision.Reason)
}

func (m *RobotManager) updateSchedulerStatus(rc robotconfig.RuntimeConfig, sig adaptiveSchedulerSignals, decision schedulerPolicyDecision) {
	target := robotconfig.NormalizedTarget(rc)
	op, opStarted, opActive := m.structuralOperation()
	if opActive {
		decision = schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: "structural_op=" + op}
	}
	recent := m.RecentOperation()
	status := robotcap.SchedulerStatus{
		Mode:                    string(decision.Mode),
		Reason:                  decision.Reason,
		RecentOperation:         recent.Type,
		RecentOperationState:    recent.State,
		RecentOperationSummary:  recentOperationSummary(recent),
		TargetOnline:            target,
		Running:                 sig.Running,
		Connecting:              sig.Connecting,
		Actors:                  sig.Actors,
		Idle:                    sig.Idle,
		ActorIdle:               sig.ActorIdle,
		ActorAssigned:           sig.ActorAssigned,
		ActorOnline:             sig.ActorOnline,
		ActorRunning:            sig.ActorRunning,
		ActorBusy:               sig.ActorBusy,
		ActorReleasing:          sig.ActorReleasing,
		StoreRunning:            sig.StoreRunning,
		GamePortReady:           sig.GamePortReady,
		BreakerActive:           sig.BreakerActive,
		CPUPercent:              sig.CPUPercent,
		MemoryMB:                sig.MemoryMB,
		Goroutines:              sig.Goroutines,
		OnlineBatchSize:         rc.SchedulerOnlineBatchSize,
		OnlineStartRate:         rc.SchedulerOnlineStartRate,
		OnlineFillTimeoutSec:    rc.SchedulerOnlineFillTimeout,
		MoveIntervalMinSec:      rc.AutoMoveIntervalMinSec,
		MoveIntervalMaxSec:      rc.AutoMoveIntervalMaxSec,
		ShoutIntervalMinSec:     rc.AutoShoutIntervalMinSec,
		ShoutIntervalMaxSec:     rc.AutoShoutIntervalMaxSec,
		StoreConcurrent:         rc.SchedulerStoreConcurrent,
		StoreProbabilityPercent: rc.AutoStoreProbabilityPercent,
		StoreIntervalMinSec:     rc.AutoStoreIntervalMinSec,
		StoreIntervalMaxSec:     rc.AutoStoreIntervalMaxSec,
		StoreDurationSec:        rc.AutoStoreDurationSec,
		StoreTickSec:            rc.AutoStoreTickSec,
		StoreMaxPositionTries:   rc.AutoStoreMaxPositionTries,
		StoreFailCooldownSec:    rc.AutoStoreFailCooldownSec,
		ScaleUpBatch:            robotconfig.ScaleUpBatch(rc),
		ScaleDownBatch:          robotconfig.ScaleDownBatch(sig.Actors, target),
		BreakerReleaseBatch:     rc.SchedulerBreakerReleaseBatch,
		PortDownReleaseBatch:    rc.SchedulerPortDownReleaseBatch,
		OperationActive:         opActive,
		Operation:               op,
		OperationStartedAt:      opStarted,
		UpdatedAt:               time.Now(),
	}
	m.autoMu.Lock()
	m.schedulerStatus = status
	m.autoMu.Unlock()
}

func recentOperationSummary(op robotcap.OperationStatus) string {
	if op.ID <= 0 {
		return ""
	}
	if op.Error != "" {
		return op.Error
	}
	return op.Summary
}

func applyAdaptiveSchedulerConfig(rc *robotconfig.RuntimeConfig, sig adaptiveSchedulerSignals) schedulerPolicyDecision {
	target := robotconfig.NormalizedTarget(*rc)
	scale := robotconfig.TargetScale(target)

	rc.SchedulerOnlineBatchSize = robotconfig.Clamp(target/10, 10, 60)
	rc.SchedulerOnlineStartRate = robotconfig.Clamp(target/40, 4, 30)
	rc.SchedulerOnlineFillTimeout = robotconfig.Clamp(target/10, 45, 180)

	rc.AutoMoveIntervalMinSec = robotconfig.Clamp(5+scale, 6, 18)
	rc.AutoMoveIntervalMaxSec = rc.AutoMoveIntervalMinSec + robotconfig.Clamp(10+scale*2, 12, 36)
	rc.AutoShoutIntervalMinSec = robotconfig.Clamp(35+scale*5, 40, 90)
	rc.AutoShoutIntervalMaxSec = rc.AutoShoutIntervalMinSec + robotconfig.Clamp(60+scale*10, 75, 180)

	rc.SchedulerStoreConcurrent = robotconfig.Clamp(target/20, 5, 50)
	rc.AutoStoreProbabilityPercent = robotconfig.Clamp(100/scale, 5, 35)
	rc.AutoStoreIntervalMinSec = robotconfig.Clamp(45+scale*5, 50, 120)
	rc.AutoStoreIntervalMaxSec = rc.AutoStoreIntervalMinSec + robotconfig.Clamp(60+scale*10, 90, 240)
	rc.AutoStoreDurationSec = robotconfig.Clamp(120+scale*15, 120, 300)
	rc.AutoStoreTickSec = robotconfig.Clamp(5+scale, 10, 30)
	rc.AutoStoreMaxPositionTries = robotconfig.Clamp(4+scale, 5, 20)
	rc.AutoStoreFailCooldownSec = robotconfig.Clamp(60+scale*15, 60, 240)

	rc.SchedulerBreakerAbnormalPct = 30
	rc.SchedulerBreakerPauseSec = robotconfig.Clamp(120+scale*30, 180, 600)
	rc.SchedulerBreakerReleaseBatch = robotconfig.Clamp(target/30, 5, 40)
	rc.SchedulerBreakerFloorPct = 70
	rc.SchedulerPortDownReleaseBatch = robotconfig.Clamp(target/25, 5, 50)

	if !sig.Live {
		return schedulerPolicyDecision{Mode: schedulerPolicyBootstrap, Reason: fmt.Sprintf("target=%d no_live_snapshot", target)}
	}
	return applyLiveSchedulerFeedback(rc, target, sig)
}

func applyLiveSchedulerFeedback(rc *robotconfig.RuntimeConfig, target int, sig adaptiveSchedulerSignals) schedulerPolicyDecision {
	if target <= 0 {
		return schedulerPolicyDecision{Mode: schedulerPolicyStable, Reason: "target_zero"}
	}
	connectingLimit := robotconfig.Clamp(target/20, 2, 30)
	healthyOnline := sig.Running >= target*95/100 && sig.Connecting <= connectingLimit && sig.GamePortReady && !sig.BreakerActive
	storeRoom := sig.StoreRunning < rc.SchedulerStoreConcurrent*7/10
	idleRoom := sig.Idle >= robotconfig.Clamp(target/50, 2, 20)
	resourcePressure := sig.CPUPercent >= 85 || sig.MemoryMB >= 4096 || sig.Goroutines >= 20000
	pendingPressure := sig.Idle >= robotconfig.PendingActorLimit(target, *rc)
	connectionPressure := sig.Connecting > robotconfig.Clamp(target/10, 3, 60)
	pressure := sig.BreakerActive || !sig.GamePortReady || resourcePressure || connectionPressure || pendingPressure

	if pressure {
		if pendingPressure && sig.GamePortReady && !sig.BreakerActive && !resourcePressure && !connectionPressure {
			rc.SchedulerOnlineBatchSize = -1
			rc.SchedulerOnlineStartRate = robotconfig.Clamp(rc.SchedulerOnlineStartRate/2, 1, 10)
			rc.SchedulerStoreConcurrent = robotconfig.Clamp(rc.SchedulerStoreConcurrent/2, 2, 25)
			rc.AutoStoreProbabilityPercent = robotconfig.Clamp(rc.AutoStoreProbabilityPercent/3, 1, 15)
			return schedulerPolicyDecision{Mode: schedulerPolicyPressure, Reason: fmt.Sprintf("pending_backlog idle=%d actors=%d running=%d target=%d", sig.Idle, sig.Actors, sig.Running, target)}
		}
		rc.SchedulerOnlineBatchSize = robotconfig.Clamp(rc.SchedulerOnlineBatchSize/2, 5, 60)
		rc.SchedulerOnlineStartRate = robotconfig.Clamp(rc.SchedulerOnlineStartRate/2, 4, 30)
		rc.SchedulerOnlineFillTimeout = robotconfig.Clamp(rc.SchedulerOnlineFillTimeout*2, 60, 300)
		rc.AutoMoveIntervalMinSec = robotconfig.Clamp(rc.AutoMoveIntervalMinSec*3/2, 10, 45)
		rc.AutoMoveIntervalMaxSec = robotconfig.Clamp(rc.AutoMoveIntervalMaxSec*3/2, rc.AutoMoveIntervalMinSec+12, 90)
		rc.AutoShoutIntervalMinSec = robotconfig.Clamp(rc.AutoShoutIntervalMinSec*3/2, 60, 180)
		rc.AutoShoutIntervalMaxSec = robotconfig.Clamp(rc.AutoShoutIntervalMaxSec*3/2, rc.AutoShoutIntervalMinSec+60, 360)
		rc.SchedulerStoreConcurrent = robotconfig.Clamp(rc.SchedulerStoreConcurrent/2, 2, 25)
		rc.AutoStoreProbabilityPercent = robotconfig.Clamp(rc.AutoStoreProbabilityPercent/3, 1, 15)
		rc.AutoStoreIntervalMinSec = robotconfig.Clamp(rc.AutoStoreIntervalMinSec*3/2, 60, 240)
		rc.AutoStoreIntervalMaxSec = robotconfig.Clamp(rc.AutoStoreIntervalMaxSec*3/2, rc.AutoStoreIntervalMinSec+60, 480)
		rc.AutoStoreFailCooldownSec = robotconfig.Clamp(rc.AutoStoreFailCooldownSec*2, 120, 600)
		rc.SchedulerBreakerReleaseBatch = robotconfig.Clamp(rc.SchedulerBreakerReleaseBatch*3/2, 5, 60)
		rc.SchedulerPortDownReleaseBatch = robotconfig.Clamp(rc.SchedulerPortDownReleaseBatch*3/2, 5, 80)
		mode := schedulerPolicyPressure
		if sig.BreakerActive {
			mode = schedulerPolicyBreaker
		}
		return schedulerPolicyDecision{Mode: mode, Reason: fmt.Sprintf("running=%d connecting=%d store=%d cpu=%.1f mem=%d goroutines=%d port=%v", sig.Running, sig.Connecting, sig.StoreRunning, sig.CPUPercent, sig.MemoryMB, sig.Goroutines, sig.GamePortReady)}
	}

	if healthyOnline && storeRoom {
		storeBoost := robotconfig.Clamp(target/40, 2, 25)
		if idleRoom {
			storeBoost += robotconfig.Clamp(sig.Idle/2, 1, 20)
		}
		rc.SchedulerStoreConcurrent = robotconfig.Clamp(rc.SchedulerStoreConcurrent+storeBoost, 5, 90)
		rc.AutoStoreProbabilityPercent = robotconfig.Clamp(rc.AutoStoreProbabilityPercent+10, 5, 60)
		rc.AutoMoveIntervalMinSec = robotconfig.Clamp(rc.AutoMoveIntervalMinSec*9/10, 5, 18)
		rc.AutoMoveIntervalMaxSec = robotconfig.Clamp(rc.AutoMoveIntervalMaxSec*9/10, rc.AutoMoveIntervalMinSec+10, 54)
		rc.AutoShoutIntervalMinSec = robotconfig.Clamp(rc.AutoShoutIntervalMinSec*9/10, 30, 90)
		rc.AutoShoutIntervalMaxSec = robotconfig.Clamp(rc.AutoShoutIntervalMaxSec*9/10, rc.AutoShoutIntervalMinSec+60, 270)
		rc.AutoStoreIntervalMinSec = robotconfig.Clamp(rc.AutoStoreIntervalMinSec*4/5, 30, 120)
		rc.AutoStoreIntervalMaxSec = robotconfig.Clamp(rc.AutoStoreIntervalMaxSec*4/5, rc.AutoStoreIntervalMinSec+60, 300)
		rc.AutoStoreMaxPositionTries = robotconfig.Clamp(rc.AutoStoreMaxPositionTries+3, 5, 30)
		return schedulerPolicyDecision{Mode: schedulerPolicyStore, Reason: fmt.Sprintf("running=%d idle=%d store=%d store_limit=%d", sig.Running, sig.Idle, sig.StoreRunning, rc.SchedulerStoreConcurrent)}
	}

	if sig.Running+sig.Connecting < target && sig.Connecting <= connectingLimit {
		rc.SchedulerOnlineBatchSize = robotconfig.Clamp(rc.SchedulerOnlineBatchSize+target/20, 20, 120)
		rc.SchedulerOnlineStartRate = robotconfig.Clamp(rc.SchedulerOnlineStartRate+target/100, 8, 60)
		return schedulerPolicyDecision{Mode: schedulerPolicyFill, Reason: fmt.Sprintf("running=%d connecting=%d target=%d", sig.Running, sig.Connecting, target)}
	}
	return schedulerPolicyDecision{Mode: schedulerPolicyStable, Reason: fmt.Sprintf("running=%d connecting=%d store=%d", sig.Running, sig.Connecting, sig.StoreRunning)}
}

func sortActorsForStopByPolicy(actors []*actormodel.Actor, status map[int]robotcap.RuntimeStatus) {
	actormodel.SortActorsForStop(actors, status)
}

var errActorRegistryUnavailable = errors.New("actor registry unavailable")

type actorRegistry interface {
	AttachUID(uid int, timeout time.Duration) bool
	Command(uid int, cmd actormodel.Command, timeout time.Duration) (robotcap.ActionResult, bool)
	EnsureActorSlots(rc robotconfig.RuntimeConfig, target int)
	HasUID(uid int) bool
	LogoutUID(uid int, timeout time.Duration) (robotcap.ActionResult, bool)
	StopUIDs(uids []int, logout bool) int
	actorSnapshots() []actormodel.Snapshot
}

type supervisorActorRegistry struct {
	supervisor *RobotSupervisor
}

var _ actorRegistry = (*supervisorActorRegistry)(nil)

func newSupervisorActorRegistry(supervisor *RobotSupervisor) *supervisorActorRegistry {
	if supervisor == nil {
		return nil
	}
	return &supervisorActorRegistry{supervisor: supervisor}
}

func (r *supervisorActorRegistry) Command(uid int, cmd actormodel.Command, timeout time.Duration) (robotcap.ActionResult, bool) {
	s := r.supervisor
	actor := s.ledger.ActorForUID(uid)
	if actor == nil {
		return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateMissingActor}, false
	}
	return actor.Enqueue(cmd, timeout)
}

func (r *supervisorActorRegistry) LogoutUID(uid int, timeout time.Duration) (robotcap.ActionResult, bool) {
	return r.Command(uid, actormodel.CommandLogout, timeout)
}

func (r *supervisorActorRegistry) AttachUID(uid int, timeout time.Duration) bool {
	s := r.supervisor
	actor, existing, ok := s.ledger.ReserveEmptyAutoActor(uid)
	if !ok {
		return false
	}
	if existing {
		return true
	}
	if actor.AssignAndWait(uid, timeout) {
		return true
	}
	s.ledger.UnleaseUID(uid, actor)
	return false
}

func (r *supervisorActorRegistry) HasUID(uid int) bool {
	return r.supervisor.ledger.HasUID(uid)
}

func (r *supervisorActorRegistry) actorSnapshots() []actormodel.Snapshot {
	s := r.supervisor
	actors := s.ledger.ActorPointers()
	out := make([]actormodel.Snapshot, 0, len(actors))
	for _, actor := range actors {
		out = append(out, actor.Snapshot())
	}
	return out
}

func (r *supervisorActorRegistry) StopUID(uid int, logout bool) bool {
	s := r.supervisor
	actor := s.ledger.DetachUID(uid)
	if actor != nil {
		if logout {
			actor.ReleaseAndWait(10 * time.Second)
		}
		actor.StopAndWait(5 * time.Second)
		return true
	}
	if logout && s.runtime != nil {
		s.runtime.Logout(uid)
	}
	return false
}

func (r *supervisorActorRegistry) StopUIDs(uids []int, logout bool) int {
	s := r.supervisor
	actors, missing := s.ledger.DetachUIDs(uids)
	if logout && s.runtime != nil {
		for _, uid := range missing {
			s.runtime.Logout(uid)
		}
	}
	stopActorsConcurrent(actors, logout)
	return len(actors)
}

func (r *supervisorActorRegistry) EnsureActorSlots(rc robotconfig.RuntimeConfig, target int) {
	r.supervisor.ensureAutoActorSlots(rc, target)
}

func (m *RobotManager) currentActorRegistry() actorRegistry {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.supervisor == nil {
		return nil
	}
	return newSupervisorActorRegistry(m.supervisor)
}
