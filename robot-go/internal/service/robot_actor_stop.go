package service

import (
	"sync"
	"time"
)

// Actor stop/release helpers.

func (s *RobotSupervisor) stopAutoActors(logout bool) {
	stopActorsConcurrent(s.ledger.detachAutoActors(), logout)
}

func (s *RobotSupervisor) stopSomeAutoActors(logout bool, limit, floor int) {
	if limit <= 0 {
		return
	}
	status := s.manager.runtimeStatusMap()
	actors := s.ledger.detachSomeAutoActors(status, limit, floor)
	if len(actors) == 0 {
		return
	}
	robotLogf("[RobotSupervisor] pressure_release actors=%d floor=%d logout=%v\n", len(actors), floor, logout)
	go stopActorsConcurrent(actors, logout)
}

func (s *RobotSupervisor) stopAll(logout bool) {
	stopActorsConcurrent(s.ledger.detachAllActors(), logout)
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
