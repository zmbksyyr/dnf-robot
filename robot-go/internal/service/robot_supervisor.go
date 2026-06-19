package service

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"robot/internal/dnf"
)

type RobotSupervisor struct {
	manager *RobotManager
	runtime *RobotRuntime

	mu         sync.Mutex
	actors     map[int]*robotActor
	uidActors  map[int]*robotActor
	blockedUID map[int]struct{}
	nextSlotID int
	stop       chan struct{}
	done       chan struct{}

	nextMetrics time.Time
	nextKeyLog  time.Time
}

func NewRobotSupervisor(manager *RobotManager, runtime *RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager:    manager,
		runtime:    runtime,
		actors:     make(map[int]*robotActor),
		uidActors:  make(map[int]*robotActor),
		blockedUID: make(map[int]struct{}),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (s *RobotSupervisor) Start() {
	go s.loop()
}

func (s *RobotSupervisor) Stop() {
	close(s.stop)
	<-s.done
}

func (s *RobotSupervisor) Command(uid int, cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool) {
	s.mu.Lock()
	actor := s.uidActors[uid]
	s.mu.Unlock()
	if actor == nil {
		return RobotActionResult{UID: uid, OK: false, State: "missing_actor"}, false
	}
	return actor.enqueue(cmd, timeout)
}

func (s *RobotSupervisor) HasUID(uid int) bool {
	if uid <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uidActors[uid] != nil
}

func (s *RobotSupervisor) StopUID(uid int, logout bool) bool {
	s.mu.Lock()
	actor := s.uidActors[uid]
	if actor != nil {
		delete(s.uidActors, uid)
		delete(s.actors, actor.slotID)
	}
	s.mu.Unlock()
	if actor != nil {
		if logout {
			actor.releaseAndWait(10 * time.Second)
		}
		actor.stopAndWait(5 * time.Second)
		return true
	}
	if logout {
		s.runtime.Logout(uid)
	}
	return false
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
	s.manager.autoMu.Lock()
	enabled := s.manager.autoEnabled
	s.manager.autoMu.Unlock()
	if !enabled || !rc.AutoActions {
		s.stopAutoActors(true)
		s.updateMetrics(rc)
		return
	}
	if st := s.manager.KeypairStatus(); !st.GameValid {
		s.stopAutoActors(true)
		s.logKeyBlocked(now, rc, st)
		s.updateMetrics(rc)
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
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc)
}

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
			if snap.UID <= 0 || snap.State == robotActorIdle || snap.State == robotActorAssigned || snap.State == robotActorOnline || snap.State == robotActorReleasing {
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

func (s *RobotSupervisor) releaseBrokenLeases() {
	type lease struct {
		uid   int
		actor *robotActor
	}
	var leases []lease
	s.mu.Lock()
	for uid, actor := range s.uidActors {
		if uid > 0 && actor != nil {
			leases = append(leases, lease{uid: uid, actor: actor})
		}
	}
	s.mu.Unlock()
	if len(leases) == 0 {
		return
	}
	uids := make([]int, 0, len(leases))
	for _, item := range leases {
		uids = append(uids, item.uid)
	}
	alive, err := s.manager.aliveRobotUIDs(uids)
	if err != nil {
		robotLogf("[RobotSupervisor] lease_health_check_failed err=%v\n", err)
		return
	}
	for _, item := range leases {
		if alive[item.uid] {
			continue
		}
		s.mu.Lock()
		if s.uidActors[item.uid] != item.actor {
			s.mu.Unlock()
			continue
		}
		delete(s.uidActors, item.uid)
		s.blockedUID[item.uid] = struct{}{}
		s.mu.Unlock()
		robotLogf("[RobotSupervisor] broken_lease uid=%d slot=%d action=release_cleanup\n", item.uid, item.actor.slotID)
		go func(actor *robotActor, uid int) {
			released := actor.releaseAndWait(10 * time.Second)
			if released != uid && released > 0 {
				s.unleaseUID(released, actor)
			}
			s.cleanupBrokenUID(uid)
		}(item.actor, item.uid)
	}
}

func (s *RobotSupervisor) cleanupBrokenUID(uid int) {
	if uid <= 0 {
		return
	}
	result, err := s.manager.CleanupRobots(RobotCleanupRequest{UIDs: []int{uid}, Force: true, InternalConfirmedBroken: true})
	if err != nil {
		robotLogf("[RobotSupervisor] broken_cleanup_failed uid=%d err=%v\n", uid, err)
		return
	}
	if result.Deleted <= 0 {
		robotLogf("[RobotSupervisor] broken_cleanup_skipped uid=%d requested=%d skipped=%d\n", uid, result.Requested, result.Skipped)
		return
	}
	s.mu.Lock()
	delete(s.blockedUID, uid)
	s.mu.Unlock()
	robotLogf("[RobotSupervisor] broken_cleanup_done uid=%d deleted=%d skipped=%d\n", uid, result.Deleted, result.Skipped)
}

func (m *RobotManager) aliveRobotUIDs(uids []int) (map[int]bool, error) {
	alive := make(map[int]bool, len(uids))
	if len(uids) == 0 {
		return alive, nil
	}
	holders := strings.TrimRight(strings.Repeat("?,", len(uids)), ",")
	args := make([]interface{}, len(uids))
	for i, uid := range uids {
		args[i] = uid
	}
	rows, err := m.db.Query("SELECT m_id FROM taiwan_cain.charac_info WHERE delete_flag=0 AND m_id IN ("+holders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		alive[uid] = true
	}
	return alive, rows.Err()
}

func (s *RobotSupervisor) recycleActorUID(actor *robotActor, status robotActorStatus) {
	if status.UID <= 0 {
		return
	}
	robotLogf("[RobotSupervisor] recycle_uid slot=%d uid=%d health=%s reason=%s failures=%d first_failure=%s state=%s\n",
		status.SlotID, status.UID, status.Health, status.HealthReason, status.Failures, status.FirstFailureAt.Format(time.RFC3339), status.State)
	released := actor.releaseAndWait(15 * time.Second)
	if released <= 0 {
		released = status.UID
	}
	s.mu.Lock()
	if s.uidActors[released] == actor {
		delete(s.uidActors, released)
	}
	s.mu.Unlock()
	result, err := s.manager.CleanupRobots(RobotCleanupRequest{UIDs: []int{released}, Force: true})
	if err != nil {
		robotLogf("[RobotSupervisor] recycle_cleanup_failed uid=%d err=%v\n", released, err)
		s.mu.Lock()
		s.blockedUID[released] = struct{}{}
		s.mu.Unlock()
		return
	}
	robotLogf("[RobotSupervisor] recycle_cleanup_done uid=%d deleted=%d skipped=%d\n", released, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) assignIdleAutoActors(rc robotRuntimeConfig) {
	idle := s.idleAutoActors()
	if len(idle) == 0 {
		return
	}
	sort.Slice(idle, func(i, j int) bool {
		return idle[i].slotID < idle[j].slotID
	})
	limit := schedulerOnlineStartRateForNeed(len(idle), rc)
	if limit > len(idle) {
		limit = len(idle)
	}
	pairs := s.acquireUIDs(rc, idle[:limit])
	for _, pair := range pairs {
		if pair.actor.assignAndWait(pair.uid, 10*time.Second) {
			continue
		}
		s.unleaseUID(pair.uid, pair.actor)
		robotLogf("[RobotSupervisor] assign_failed slot=%d uid=%d\n", pair.actor.slotID, pair.uid)
	}
}

func (s *RobotSupervisor) idleAutoActors() []*robotActor {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*robotActor{}
	for _, actor := range s.actors {
		snap := actor.snapshot()
		if snap.Mode == robotActorAuto && snap.UID <= 0 {
			out = append(out, actor)
		}
	}
	return out
}

type robotActorLease struct {
	actor *robotActor
	uid   int
}

func (s *RobotSupervisor) acquireUIDs(rc robotRuntimeConfig, actors []*robotActor) []robotActorLease {
	if len(actors) == 0 {
		return nil
	}
	robots, err := s.manager.selectRobots(RobotCommandRequest{Count: rc.MaxOnlineRobots})
	if err != nil {
		robotLogf("[RobotSupervisor] select_robots_failed err=%v\n", err)
		return nil
	}
	out := make([]robotActorLease, 0, len(actors))
	nextActor := 0
	for _, robot := range robots {
		if nextActor >= len(actors) {
			return out
		}
		actor := actors[nextActor]
		if s.tryLeaseUID(robot.UID, actor) {
			out = append(out, robotActorLease{actor: actor, uid: robot.UID})
			nextActor++
		}
	}
	need := len(actors) - nextActor
	if need <= 0 {
		return out
	}
	created, err := s.manager.CreateRobots(RobotCreateRequest{Count: need})
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
		if s.tryLeaseUID(robot.UID, actor) {
			out = append(out, robotActorLease{actor: actor, uid: robot.UID})
			nextActor++
		}
	}
	return out
}

func (s *RobotSupervisor) tryLeaseUID(uid int, actor *robotActor) bool {
	if uid <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, blocked := s.blockedUID[uid]; blocked {
		return false
	}
	if _, leased := s.uidActors[uid]; leased {
		return false
	}
	s.uidActors[uid] = actor
	return true
}

func (s *RobotSupervisor) unleaseUID(uid int, actor *robotActor) {
	if uid <= 0 {
		return
	}
	s.mu.Lock()
	if actor == nil || s.uidActors[uid] == actor || s.uidActors[uid] == nil {
		delete(s.uidActors, uid)
	}
	s.mu.Unlock()
}

func schedulerOnlineStartRate(rc robotRuntimeConfig) int {
	rate := rc.SchedulerOnlineStartRate
	if rate < 0 {
		return 0
	}
	if rate <= 0 {
		rate = 20
	}
	if rate > 60 {
		rate = 60
	}
	return rate
}

func schedulerOnlineStartRateForNeed(need int, rc robotRuntimeConfig) int {
	rate := schedulerOnlineStartRate(rc)
	if rate <= 0 {
		return 0
	}
	if need <= 0 {
		return rate
	}
	timeout := rc.SchedulerOnlineFillTimeout
	if timeout <= 0 {
		timeout = 60
	}
	required := (need + timeout - 1) / timeout
	if required > rate {
		rate = required
	}
	if rate > 60 {
		return 60
	}
	return rate
}

func (s *RobotSupervisor) nextSlotLocked() int {
	s.nextSlotID++
	return s.nextSlotID
}

func (s *RobotSupervisor) stopAutoActors(logout bool) {
	s.mu.Lock()
	var actors []*robotActor
	for slotID, actor := range s.actors {
		if actor.modeValue() != robotActorAuto {
			continue
		}
		actors = append(actors, actor)
		delete(s.actors, slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(s.uidActors, uid)
		}
	}
	s.mu.Unlock()
	stopActorsConcurrent(actors, logout)
}

func (s *RobotSupervisor) stopSomeAutoActors(logout bool, limit, floor int) {
	if limit <= 0 {
		return
	}
	status := s.manager.runtimeStatusMap()
	s.mu.Lock()
	var candidates []*robotActor
	for _, actor := range s.actors {
		if actor.modeValue() != robotActorAuto {
			continue
		}
		candidates = append(candidates, actor)
	}
	sortActorsForStopByPolicy(candidates, status)
	if floor < 0 {
		floor = 0
	}
	if len(candidates) <= floor {
		s.mu.Unlock()
		return
	}
	maxStop := len(candidates) - floor
	if limit > maxStop {
		limit = maxStop
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	actors := candidates[:limit]
	for _, actor := range actors {
		delete(s.actors, actor.slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(s.uidActors, uid)
		}
	}
	s.mu.Unlock()
	if len(actors) == 0 {
		return
	}
	robotLogf("[RobotSupervisor] pressure_release actors=%d floor=%d logout=%v\n", len(actors), floor, logout)
	go stopActorsConcurrent(actors, logout)
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

func (s *RobotSupervisor) stopAll(logout bool) {
	s.mu.Lock()
	actors := make([]*robotActor, 0, len(s.actors))
	for slotID, actor := range s.actors {
		actors = append(actors, actor)
		delete(s.actors, slotID)
	}
	s.uidActors = make(map[int]*robotActor)
	s.mu.Unlock()
	stopActorsConcurrent(actors, logout)
}

func stopActorsConcurrent(actors []*robotActor, logout bool) {
	var wg sync.WaitGroup
	for _, actor := range actors {
		wg.Add(1)
		go func(actor *robotActor) {
			defer wg.Done()
			if logout {
				actor.releaseAndWait(10 * time.Second)
			}
			actor.stopAndWait(5 * time.Second)
		}(actor)
	}
	wg.Wait()
}

func (s *RobotSupervisor) updateMetrics(rc robotRuntimeConfig) {
	now := time.Now()
	if !s.nextMetrics.IsZero() && now.Before(s.nextMetrics) {
		return
	}
	s.nextMetrics = now.Add(time.Duration(rc.SchedulerMetricsIntervalSec) * time.Second)
	status := s.manager.runtimeStatusMap()
	s.filterBlockedRuntimeStatus(status)
	running, connecting, stores := summarizeRuntimeStatusMap(status)
	s.manager.updateAutoSnapshot(rc, running, connecting, stores)
	counts := s.actorCounts(now, rc)
	s.manager.updateAutoActorSnapshot(counts)
	s.manager.updateAutoBreaker(now, rc, counts, running, connecting)
	cpu, mem, threads := robotResourceSnapshot()
	s.manager.autoMu.Lock()
	stats := s.manager.autoStats
	policy := s.manager.schedulerStatus
	s.manager.autoMu.Unlock()
	line := fmt.Sprintf("[RobotMetrics] policy=%s target=%d actors=%d leased=%d idle=%d state idle=%d assigned=%d online=%d running=%d busy=%d releasing=%d runtime running=%d store=%d connecting=%d recycling=%d blocked=%d cpu=%.1f mem_mb=%d goroutines=%d online=%d/%d move=%d/%d shout_local=%d/%d shout_world=%d/%d store=%d/%d expired=%d\n",
		policy.Mode,
		rc.AutoTargetOnlineCount, counts.auto, counts.leased, counts.idle,
		counts.stateIdle, counts.stateAssigned, counts.stateOnline, counts.stateRunning, counts.stateBusy, counts.stateReleasing,
		running, stores, connecting, counts.releasing, counts.blocked,
		cpu, mem, threads,
		stats.OnlineSuccess, stats.OnlineFailed,
		stats.MoveSuccess, stats.MoveFailed,
		stats.ShoutLocalSuccess, stats.ShoutLocalFailed,
		stats.ShoutWorldSuccess, stats.ShoutWorldFailed,
		stats.StoreSuccess, stats.StoreFailed, stats.StoreExpired)
	fmt.Print(line)
	dnf.LogString(dnf.LogLevelIndispensable, line)
}

func (s *RobotSupervisor) filterBlockedRuntimeStatus(status map[int]RuntimeRobotStatus) {
	if len(status) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for uid := range s.blockedUID {
		delete(status, uid)
	}
}

type supervisorActorCounts struct {
	auto           int
	leased         int
	idle           int
	releasing      int
	blocked        int
	stateIdle      int
	stateAssigned  int
	stateOnline    int
	stateRunning   int
	stateBusy      int
	stateReleasing int
}

func (s *RobotSupervisor) actorCounts(now time.Time, rc robotRuntimeConfig) supervisorActorCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := supervisorActorCounts{blocked: len(s.blockedUID)}
	for _, actor := range s.actors {
		status := actor.status(now, rc)
		if status.Mode != robotActorAuto {
			continue
		}
		counts.auto++
		if status.UID > 0 {
			counts.leased++
		} else {
			counts.idle++
		}
		if status.State == robotActorReleasing {
			counts.releasing++
		}
		switch status.State {
		case robotActorIdle:
			counts.stateIdle++
		case robotActorAssigned:
			counts.stateAssigned++
		case robotActorOnline:
			counts.stateOnline++
		case robotActorRunning:
			counts.stateRunning++
		case robotActorBusy:
			counts.stateBusy++
		case robotActorReleasing:
			counts.stateReleasing++
		}
	}
	return counts
}
