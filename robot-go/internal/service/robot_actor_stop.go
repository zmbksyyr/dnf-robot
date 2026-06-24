package service

import (
	"sync"
	"time"
)

// Actor stop/release helpers.

const actorStopConcurrency = 60

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
	if len(actors) == 0 {
		return
	}
	workers := actorStopConcurrency
	if len(actors) < workers {
		workers = len(actors)
	}
	jobs := make(chan *robotActor)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for actor := range jobs {
				if logout {
					actor.releaseAndWait(10 * time.Second)
				}
				actor.stopAndWait(5 * time.Second)
			}
		}()
	}
	for _, actor := range actors {
		jobs <- actor
	}
	close(jobs)
	wg.Wait()
}
