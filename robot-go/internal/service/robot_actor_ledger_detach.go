package service

func (l *actorLedger) detachAutoActors() []*robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	actors := make([]*robotActor, 0, len(l.actors))
	for slotID, actor := range l.actors {
		if actor.modeValue() != robotActorAuto {
			continue
		}
		actors = append(actors, actor)
		delete(l.actors, slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(l.uidActors, uid)
		}
	}
	return actors
}

func (l *actorLedger) detachSomeAutoActors(status map[int]RuntimeRobotStatus, limit, floor int) []*robotActor {
	if limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var candidates []*robotActor
	for _, actor := range l.actors {
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
		return nil
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
		delete(l.actors, actor.slotID)
		if uid := actor.uidValue(); uid > 0 {
			delete(l.uidActors, uid)
		}
	}
	return actors
}

func (l *actorLedger) detachAllActors() []*robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	actors := make([]*robotActor, 0, len(l.actors))
	for slotID, actor := range l.actors {
		actors = append(actors, actor)
		delete(l.actors, slotID)
	}
	l.uidActors = make(map[int]*robotActor)
	return actors
}

func (l *actorLedger) detachUID(uid int) *robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	actor := l.uidActors[uid]
	if actor != nil {
		delete(l.uidActors, uid)
		delete(l.actors, actor.slotID)
	}
	return actor
}

func (l *actorLedger) detachUIDs(uids []int) ([]*robotActor, []int) {
	if len(uids) == 0 {
		return nil, nil
	}
	seen := make(map[int]struct{}, len(uids))
	actors := make([]*robotActor, 0, len(uids))
	missing := make([]int, 0)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, uid := range uids {
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		actor := l.uidActors[uid]
		if actor == nil {
			missing = append(missing, uid)
			continue
		}
		delete(l.uidActors, uid)
		delete(l.actors, actor.slotID)
		delete(l.blockedUID, uid)
		actors = append(actors, actor)
	}
	return actors, missing
}
