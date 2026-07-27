package actor

import (
	"sort"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/lockhub"
)

type Ledger struct {
	indexMu    lockhub.Locker
	actors     map[int]*Actor
	uidActors  map[int]*Actor
	draining   map[int]*Actor
	blockedUID map[int]struct{}
	nextSlotID int
}

type LeaseSnapshot struct {
	UID     int
	Actor   *Actor
	Blocked bool
}

type LedgerCounts struct {
	Auto           int
	Leased         int
	Idle           int
	Draining       int
	Releasing      int
	Blocked        int
	StateIdle      int
	StateAssigned  int
	StateOnline    int
	StateRunning   int
	StateBusy      int
	StateReleasing int
}

func NewLedger() Ledger {
	return Ledger{
		actors:     make(map[int]*Actor),
		uidActors:  make(map[int]*Actor),
		draining:   make(map[int]*Actor),
		blockedUID: make(map[int]struct{}),
	}
}

func (l *Ledger) LeaseSnapshots() []LeaseSnapshot {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	leases := make([]LeaseSnapshot, 0, len(l.uidActors))
	for uid, actor := range l.uidActors {
		if uid > 0 && actor != nil && l.draining[actor.slotIDValue()] != actor {
			_, blocked := l.blockedUID[uid]
			leases = append(leases, LeaseSnapshot{UID: uid, Actor: actor, Blocked: blocked})
		}
	}
	return leases
}

func (l *Ledger) Counts(now time.Time, rc robotconfig.RuntimeConfig) LedgerCounts {
	counts := LedgerCounts{Blocked: l.BlockedCount()}
	l.indexMu.Lock()
	actors := make([]*Actor, 0, len(l.actors))
	draining := make(map[int]*Actor, len(l.draining))
	for slotID, actor := range l.actors {
		if actor.ModeValue() == ModeAuto {
			actors = append(actors, actor)
		}
		if current := l.draining[slotID]; current != nil {
			draining[slotID] = current
		}
	}
	l.indexMu.Unlock()
	for _, actor := range actors {
		status := actor.Status(now, rc)
		counts.Auto++
		if status.UID > 0 {
			counts.Leased++
		} else {
			counts.Idle++
		}
		isDraining := draining[actor.SlotIDValue()] == actor
		if isDraining {
			counts.Draining++
			counts.Releasing++
			counts.StateReleasing++
			continue
		}
		if status.State == StateReleasing {
			counts.Releasing++
		}
		switch status.State {
		case StateIdle:
			counts.StateIdle++
		case StateOffline:
			counts.StateAssigned++
		case StateAssigned:
			counts.StateAssigned++
		case StateOnline:
			counts.StateOnline++
		case StateRunning:
			counts.StateRunning++
		case StateBusy:
			counts.StateBusy++
		case StateReleasing:
			counts.StateReleasing++
		}
	}
	return counts
}

func (l *Ledger) BlockLeaseIfCurrent(uid int, actor *Actor) bool {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	if l.uidActors[uid] != actor {
		return false
	}
	l.blockedUID[uid] = struct{}{}
	return true
}

func (l *Ledger) UnblockUID(uid int) {
	l.indexMu.Lock()
	delete(l.blockedUID, uid)
	l.indexMu.Unlock()
}

func (l *Ledger) BlockedUIDs(limit int) []int {
	if limit <= 0 {
		return nil
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	uids := make([]int, 0, limit)
	for uid := range l.blockedUID {
		uids = append(uids, uid)
		if len(uids) >= limit {
			break
		}
	}
	return uids
}

func (l *Ledger) BlockedCount() int {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return len(l.blockedUID)
}

func (l *Ledger) TryLeaseUID(uid int, actor *Actor) bool {
	if uid <= 0 || actor == nil {
		return false
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	if l.draining[actor.SlotIDValue()] == actor {
		return false
	}
	if _, blocked := l.blockedUID[uid]; blocked {
		return false
	}
	if _, leased := l.uidActors[uid]; leased {
		return false
	}
	l.uidActors[uid] = actor
	return true
}

func (l *Ledger) UnleaseUID(uid int, actor *Actor) {
	if uid <= 0 {
		return
	}
	l.indexMu.Lock()
	if actor == nil || l.uidActors[uid] == actor || l.uidActors[uid] == nil {
		delete(l.uidActors, uid)
	}
	l.indexMu.Unlock()
}

func (l *Ledger) RemoveLeaseIfActor(uid int, actor *Actor) {
	l.indexMu.Lock()
	if l.uidActors[uid] == actor {
		delete(l.uidActors, uid)
	}
	l.indexMu.Unlock()
}

func (l *Ledger) BlockUID(uid int) {
	if uid <= 0 {
		return
	}
	l.indexMu.Lock()
	l.blockedUID[uid] = struct{}{}
	l.indexMu.Unlock()
}

func (l *Ledger) EnsureAutoActorSlots(runtime RobotRuntime, rc robotconfig.RuntimeConfig, target int, status map[int]robotcap.RuntimeStatus) []*Actor {
	if target < 0 {
		target = 0
	}
	l.indexMu.Lock()
	l.reapDoneDrainingLocked()
	current := 0
	pending := 0
	for slotID, actor := range l.actors {
		if actor.ModeValue() == ModeAuto {
			current++
			if l.draining[slotID] != actor && SnapshotSchedulerPending(actor.Snapshot()) {
				pending++
			}
		}
	}
	addCount := target - current
	if addCount < 0 {
		addCount = 0
	}
	pendingLimit := robotconfig.PendingActorLimit(target, rc)
	if pending >= pendingLimit {
		addCount = 0
	}
	if addCount > robotconfig.ScaleUpBatch(rc) {
		addCount = robotconfig.ScaleUpBatch(rc)
	}
	added := addCount
	for addCount > 0 {
		slotID := l.nextSlotLocked()
		actor := NewActor(slotID, ModeAuto, runtime)
		l.actors[slotID] = actor
		actor.Start()
		addCount--
	}
	current += added
	if current <= target {
		l.indexMu.Unlock()
		return nil
	}
	var candidates []*Actor
	for slotID, actor := range l.actors {
		if actor.ModeValue() == ModeAuto && l.draining[slotID] != actor {
			candidates = append(candidates, actor)
		}
	}
	SortActorsForStop(candidates, status)
	removeCount := current - target
	if removeCount > robotconfig.ScaleDownBatch(current, target) {
		removeCount = robotconfig.ScaleDownBatch(current, target)
	}
	if removeCount > len(candidates) {
		removeCount = len(candidates)
	}
	extra := candidates[:removeCount]
	for _, actor := range extra {
		l.draining[actor.SlotIDValue()] = actor
	}
	l.indexMu.Unlock()
	return extra
}

func (l *Ledger) ActorPointers() []*Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	actors := make([]*Actor, 0, len(l.actors))
	for _, actor := range l.actors {
		actors = append(actors, actor)
	}
	return actors
}

func (l *Ledger) ActorForUID(uid int) *Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return l.uidActors[uid]
}

func (l *Ledger) HasUID(uid int) bool {
	if uid <= 0 {
		return false
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return l.uidActors[uid] != nil
}

func (l *Ledger) ReserveEmptyAutoActor(uid int) (*Actor, bool, bool) {
	if uid <= 0 {
		return nil, false, false
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	if _, blocked := l.blockedUID[uid]; blocked {
		return nil, false, false
	}
	if existing := l.uidActors[uid]; existing != nil {
		return existing, true, true
	}
	var actor *Actor
	for slotID, candidate := range l.actors {
		if l.draining[slotID] == candidate {
			continue
		}
		snap := candidate.Snapshot()
		if snap.Mode == ModeAuto && snap.UID <= 0 {
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

func (l *Ledger) AutoActorPointers() []*Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	actors := make([]*Actor, 0, len(l.actors))
	for slotID, actor := range l.actors {
		if actor.ModeValue() == ModeAuto && l.draining[slotID] != actor {
			actors = append(actors, actor)
		}
	}
	return actors
}

func (l *Ledger) IdleAutoActors() []*Actor {
	actors := l.AutoActorPointers()
	out := make([]*Actor, 0, len(actors))
	for _, actor := range actors {
		if SnapshotEmpty(actor.Snapshot()) {
			out = append(out, actor)
		}
	}
	return out
}

func (l *Ledger) FilterBlockedRuntimeStatus(status map[int]robotcap.RuntimeStatus) {
	if len(status) == 0 {
		return
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	for uid := range l.blockedUID {
		delete(status, uid)
	}
}

func (l *Ledger) BeginDrainAutoActors() []*Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	actors := make([]*Actor, 0, len(l.actors))
	for slotID, actor := range l.actors {
		if actor.ModeValue() != ModeAuto || l.draining[slotID] == actor {
			continue
		}
		actors = append(actors, actor)
		l.draining[slotID] = actor
	}
	return actors
}

func (l *Ledger) BeginDrainSomeAutoActors(status map[int]robotcap.RuntimeStatus, limit, floor int) []*Actor {
	if limit <= 0 {
		return nil
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	var candidates []*Actor
	for slotID, actor := range l.actors {
		if actor.ModeValue() == ModeAuto && l.draining[slotID] != actor {
			candidates = append(candidates, actor)
		}
	}
	SortActorsForStop(candidates, status)
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
		l.draining[actor.SlotIDValue()] = actor
	}
	return actors
}

func (l *Ledger) BeginDrainAllActors() []*Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	actors := make([]*Actor, 0, len(l.actors))
	for slotID, actor := range l.actors {
		actors = append(actors, actor)
		l.draining[slotID] = actor
	}
	return actors
}

func (l *Ledger) BeginDrainUID(uid int) *Actor {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	actor := l.uidActors[uid]
	if actor != nil {
		l.draining[actor.SlotIDValue()] = actor
	}
	return actor
}

func (l *Ledger) BeginDrainUIDs(uids []int) ([]*Actor, []int) {
	if len(uids) == 0 {
		return nil, nil
	}
	seen := make(map[int]struct{}, len(uids))
	actors := make([]*Actor, 0, len(uids))
	missing := make([]int, 0)
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
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
		l.draining[actor.SlotIDValue()] = actor
		actors = append(actors, actor)
	}
	return actors, missing
}

func (l *Ledger) ReapActor(actor *Actor) bool {
	if actor == nil || !actorDone(actor) {
		return false
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	if l.draining[actor.SlotIDValue()] != actor {
		return true
	}
	return l.reapActorLocked(actor)
}

func (l *Ledger) ReapDoneDraining() int {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return l.reapDoneDrainingLocked()
}

func (l *Ledger) IsDraining(actor *Actor) bool {
	if actor == nil {
		return false
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return l.draining[actor.SlotIDValue()] == actor
}

func (l *Ledger) DrainingCount() int {
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	return len(l.draining)
}

func (l *Ledger) reapDoneDrainingLocked() int {
	reaped := 0
	for _, actor := range l.draining {
		if actorDone(actor) && l.reapActorLocked(actor) {
			reaped++
		}
	}
	return reaped
}

func (l *Ledger) reapActorLocked(actor *Actor) bool {
	slotID := actor.SlotIDValue()
	if l.draining[slotID] != actor || !actorDone(actor) {
		return false
	}
	delete(l.draining, slotID)
	if l.actors[slotID] == actor {
		delete(l.actors, slotID)
	}
	for uid, leased := range l.uidActors {
		if leased == actor {
			delete(l.uidActors, uid)
			delete(l.blockedUID, uid)
		}
	}
	return true
}

func actorDone(actor *Actor) bool {
	select {
	case <-actor.Done():
		return true
	default:
		return false
	}
}

func (l *Ledger) ActorOwnsUID(uid int) bool {
	if uid <= 0 {
		return false
	}
	if l.ActorForUID(uid) != nil {
		return true
	}
	l.indexMu.Lock()
	defer l.indexMu.Unlock()
	for _, actor := range l.actors {
		if actor.UIDValue() == uid {
			return true
		}
	}
	return false
}

func (l *Ledger) nextSlotLocked() int {
	l.nextSlotID++
	return l.nextSlotID
}

func SortActorsForStop(actors []*Actor, status map[int]robotcap.RuntimeStatus) {
	sort.Slice(actors, func(i, j int) bool {
		leftPriority := StopPriority(actors[i].UIDValue(), status)
		rightPriority := StopPriority(actors[j].UIDValue(), status)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftUID := actors[i].UIDValue()
		rightUID := actors[j].UIDValue()
		if leftUID <= 0 || rightUID <= 0 {
			if leftUID != rightUID {
				return leftUID <= 0
			}
			return actors[i].SlotIDValue() > actors[j].SlotIDValue()
		}
		if leftUID != rightUID {
			return leftUID > rightUID
		}
		return actors[i].SlotIDValue() > actors[j].SlotIDValue()
	})
}
