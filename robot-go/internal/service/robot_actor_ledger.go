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
