package service

import (
	"sync"
	"time"
)

// Actor stop/release helpers.

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
