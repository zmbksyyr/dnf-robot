package scheduler

import (
	"sort"
	"sync"
	"time"

	actormodel "robot/internal/actor"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

const actorStopConcurrency = 60

func (s *RobotSupervisor) stopAutoActors(logout bool) {
	stopActorsConcurrent(s.ledger.DetachAutoActors(), logout)
}

func (s *RobotSupervisor) stopSomeAutoActors(logout bool, limit, floor int) {
	if limit <= 0 {
		return
	}
	s.pressureMu.Lock()
	if s.pressureRunning {
		s.pressureMu.Unlock()
		return
	}
	s.pressureRunning = true
	s.pressureMu.Unlock()

	status := s.manager.runtimeStatusMap()
	actors := s.ledger.DetachSomeAutoActors(status, limit, floor)
	if len(actors) == 0 {
		s.pressureMu.Lock()
		s.pressureRunning = false
		s.pressureMu.Unlock()
		return
	}
	robotLogf("[RobotSupervisor] pressure_release actors=%d floor=%d logout=%v\n", len(actors), floor, logout)
	s.stopWG.Add(1)
	go func() {
		defer func() {
			s.pressureMu.Lock()
			s.pressureRunning = false
			s.pressureMu.Unlock()
			s.stopWG.Done()
		}()
		stopActorsConcurrent(actors, logout)
	}()
}

func (s *RobotSupervisor) stopAll(logout bool) {
	stopActorsConcurrentFully(s.ledger.DetachAllActors(), logout)
}

func stopActorsConcurrent(actors []*actormodel.Actor, logout bool) {
	stopActorsConcurrentWithWait(actors, logout, 5*time.Second)
}

func stopActorsConcurrentFully(actors []*actormodel.Actor, logout bool) {
	stopActorsConcurrentWithWait(actors, logout, 0)
}

func stopActorsConcurrentWithWait(actors []*actormodel.Actor, logout bool, stopWait time.Duration) {
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
				actor.StopAndWait(stopWait)
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
