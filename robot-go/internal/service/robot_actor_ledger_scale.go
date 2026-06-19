package service

func (l *actorLedger) ensureAutoActorSlots(runtime *RobotRuntime, rc robotRuntimeConfig, target int, status map[int]RuntimeRobotStatus) []*robotActor {
	if target < 0 {
		target = 0
	}
	l.mu.Lock()
	current := 0
	pending := 0
	for _, actor := range l.actors {
		if actor.modeValue() == robotActorAuto {
			current++
			snap := actor.snapshot()
			if snap.schedulerPending() {
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
		slotID := l.nextSlotLocked()
		actor := newRobotActor(slotID, robotActorAuto, runtime)
		l.actors[slotID] = actor
		actor.start()
		addCount--
	}
	current += added
	if current <= target {
		l.mu.Unlock()
		return nil
	}
	var candidates []*robotActor
	for _, actor := range l.actors {
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
		delete(l.actors, actor.slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(l.uidActors, uid)
		}
	}
	l.mu.Unlock()
	return extra
}
