package service

import "sync"

// actorLedger is the supervisor-owned state table for actor slots, UID leases,
// blocked UIDs, and slot allocation. It is embedded in RobotSupervisor today so
// the extraction can stay mechanical while the call sites are narrowed.
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

func (l *actorLedger) blockLeaseIfCurrent(uid int, actor *robotActor) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.uidActors[uid] != actor {
		return false
	}
	delete(l.uidActors, uid)
	l.blockedUID[uid] = struct{}{}
	return true
}

func (l *actorLedger) unblockUID(uid int) {
	l.mu.Lock()
	delete(l.blockedUID, uid)
	l.mu.Unlock()
}

func (l *actorLedger) blockedUIDs(limit int) []int {
	if limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	uids := make([]int, 0, limit)
	for uid := range l.blockedUID {
		uids = append(uids, uid)
		if len(uids) >= limit {
			break
		}
	}
	return uids
}

func (l *actorLedger) blockedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.blockedUID)
}

func (l *actorLedger) tryLeaseUID(uid int, actor *robotActor) bool {
	if uid <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, blocked := l.blockedUID[uid]; blocked {
		return false
	}
	if _, leased := l.uidActors[uid]; leased {
		return false
	}
	l.uidActors[uid] = actor
	return true
}

func (l *actorLedger) unleaseUID(uid int, actor *robotActor) {
	if uid <= 0 {
		return
	}
	l.mu.Lock()
	if actor == nil || l.uidActors[uid] == actor || l.uidActors[uid] == nil {
		delete(l.uidActors, uid)
	}
	l.mu.Unlock()
}

func (l *actorLedger) removeLeaseIfActor(uid int, actor *robotActor) {
	l.mu.Lock()
	if l.uidActors[uid] == actor {
		delete(l.uidActors, uid)
	}
	l.mu.Unlock()
}

func (l *actorLedger) blockUID(uid int) {
	if uid <= 0 {
		return
	}
	l.mu.Lock()
	l.blockedUID[uid] = struct{}{}
	l.mu.Unlock()
}
