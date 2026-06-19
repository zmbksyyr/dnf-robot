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
		s.actorLedger.unleaseUID(pair.uid, pair.actor)
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
		if s.actorLedger.tryLeaseUID(robot.UID, actor) {
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
		robotLogf("[RobotSupervisor] create_blocked_by_target existing=%d target=%d need=%d blocked=%d\n", len(robots), target, need, s.actorLedger.blockedCount())
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
		if s.actorLedger.tryLeaseUID(robot.UID, actor) {
			out = append(out, robotActorLease{actor: actor, uid: robot.UID})
			nextActor++
		}
	}
	return out
}
