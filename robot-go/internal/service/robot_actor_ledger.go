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

func newActorLedger() actorLedger {
	return actorLedger{
		actors:     make(map[int]*robotActor),
		uidActors:  make(map[int]*robotActor),
		blockedUID: make(map[int]struct{}),
	}
}
