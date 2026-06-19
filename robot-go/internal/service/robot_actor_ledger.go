package service

import "sync"

// actorLedger is the supervisor-owned state table for actor slots, UID leases,
// blocked UIDs, and slot allocation.
type actorLedger struct {
	mu         sync.Mutex
	actors     map[int]*robotActor
	uidActors  map[int]*robotActor
	blockedUID map[int]struct{}
	nextSlotID int
}

type actorLeaseSnapshot struct {
	uid   int
	actor *robotActor
}

func newActorLedger() actorLedger {
	return actorLedger{
		actors:     make(map[int]*robotActor),
		uidActors:  make(map[int]*robotActor),
		blockedUID: make(map[int]struct{}),
	}
}

func (l *actorLedger) leaseSnapshots() []actorLeaseSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	leases := make([]actorLeaseSnapshot, 0, len(l.uidActors))
	for uid, actor := range l.uidActors {
		if uid > 0 && actor != nil {
			leases = append(leases, actorLeaseSnapshot{uid: uid, actor: actor})
		}
	}
	return leases
}

func (l *actorLedger) actorPointers() []*robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	actors := make([]*robotActor, 0, len(l.actors))
	for _, actor := range l.actors {
		actors = append(actors, actor)
	}
	return actors
}

func (l *actorLedger) actorForUID(uid int) *robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.uidActors[uid]
}

func (l *actorLedger) hasUID(uid int) bool {
	if uid <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.uidActors[uid] != nil
}

func (l *actorLedger) reserveEmptyAutoActor(uid int) (*robotActor, bool, bool) {
	if uid <= 0 {
		return nil, false, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing := l.uidActors[uid]; existing != nil {
		return existing, true, true
	}
	if _, blocked := l.blockedUID[uid]; blocked {
		return nil, false, false
	}
	var actor *robotActor
	for _, candidate := range l.actors {
		snap := candidate.snapshot()
		if snap.Mode == robotActorAuto && snap.UID <= 0 {
			actor = candidate
			break
		}
	}
	if actor == nil {
		return nil, false, false
	}
	l.uidActors[uid] = actor
	return actor, false, true
}

func (l *actorLedger) autoActorPointers() []*robotActor {
	l.mu.Lock()
	defer l.mu.Unlock()
	actors := make([]*robotActor, 0, len(l.actors))
	for _, actor := range l.actors {
		if actor.modeValue() == robotActorAuto {
			actors = append(actors, actor)
		}
	}
	return actors
}

func (l *actorLedger) idleAutoActors() []*robotActor {
	actors := l.autoActorPointers()
	out := make([]*robotActor, 0, len(actors))
	for _, actor := range actors {
		if actor.snapshot().empty() {
			out = append(out, actor)
		}
	}
	return out
}

func (l *actorLedger) filterBlockedRuntimeStatus(status map[int]RuntimeRobotStatus) {
	if len(status) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for uid := range l.blockedUID {
		delete(status, uid)
	}
}

func (l *actorLedger) nextSlotLocked() int {
	l.nextSlotID++
	return l.nextSlotID
}
