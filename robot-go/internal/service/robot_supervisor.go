package service

import (
	"fmt"
	"sort"
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
		s.updateMetrics(rc)
		return
	}
	s.maintainTarget(rc)
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
	s.ensureAutoActorSlots(target)
}

func (s *RobotSupervisor) ensureAutoActorSlots(target int) {
	if target < 0 {
		target = 0
	}
	s.mu.Lock()
	current := 0
	for _, actor := range s.actors {
		if actor.modeValue() == robotActorAuto {
			current++
		}
	}
	for current < target {
		slotID := s.nextSlotLocked()
		actor := newRobotActor(slotID, robotActorAuto, s.runtime)
		s.actors[slotID] = actor
		actor.start()
		current++
	}
	var candidates []*robotActor
	for _, actor := range s.actors {
		if actor.modeValue() != robotActorAuto {
			continue
		}
		candidates = append(candidates, actor)
	}
	sortActorsForStop(candidates)
	removeCount := current - target
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

func sortActorsForStop(actors []*robotActor) {
	sort.Slice(actors, func(i, j int) bool {
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
	running, connecting, stores := summarizeRuntimeStatusMap(status)
	s.manager.updateAutoSnapshot(rc, running, connecting, stores)
	counts := s.actorCounts(now, rc)
	s.manager.updateAutoActorSnapshot(counts.auto, counts.leased, counts.idle, counts.releasing, counts.blocked)
	cpu, mem, threads := robotResourceSnapshot()
	s.manager.autoMu.Lock()
	stats := s.manager.autoStats
	s.manager.autoMu.Unlock()
	line := fmt.Sprintf("[RobotMetrics] target=%d actors=%d leased=%d idle=%d running=%d store=%d connecting=%d recycling=%d blocked=%d cpu=%.1f mem_mb=%d goroutines=%d online=%d/%d move=%d/%d shout_local=%d/%d shout_world=%d/%d store=%d/%d expired=%d\n",
		rc.AutoTargetOnlineCount, counts.auto, counts.leased, counts.idle, running, stores, connecting, counts.releasing, counts.blocked,
		cpu, mem, threads,
		stats.OnlineSuccess, stats.OnlineFailed,
		stats.MoveSuccess, stats.MoveFailed,
		stats.ShoutLocalSuccess, stats.ShoutLocalFailed,
		stats.ShoutWorldSuccess, stats.ShoutWorldFailed,
		stats.StoreSuccess, stats.StoreFailed, stats.StoreExpired)
	fmt.Print(line)
	dnf.LogString(dnf.LogLevelIndispensable, line)
}

type supervisorActorCounts struct {
	auto      int
	leased    int
	idle      int
	releasing int
	blocked   int
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
	}
	return counts
}
