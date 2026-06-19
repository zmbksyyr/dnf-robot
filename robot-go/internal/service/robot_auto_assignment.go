package service

import (
	"sort"
	"time"
)

// Assignment and lease helpers.

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
		if snap.Mode == robotActorAuto && snap.empty() {
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
	target := schedulerTargetCapacity(rc)
	createRoom := schedulerCreateRoom(rc, len(robots))
	if createRoom <= 0 {
		robotLogf("[RobotSupervisor] create_blocked_by_target existing=%d target=%d need=%d blocked=%d\n", len(robots), target, need, s.blockedCount())
		return out
	}
	if need > createRoom {
		need = createRoom
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

func schedulerTargetCapacity(rc robotRuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	return target
}

func schedulerCreateRoom(rc robotRuntimeConfig, existing int) int {
	room := schedulerTargetCapacity(rc) - existing
	if room < 0 {
		return 0
	}
	return room
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
