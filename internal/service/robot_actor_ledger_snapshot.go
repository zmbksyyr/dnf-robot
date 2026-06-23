package service

import "time"

type actorLedgerCounts struct {
	auto           int
	leased         int
	idle           int
	releasing      int
	blocked        int
	stateIdle      int
	stateAssigned  int
	stateOnline    int
	stateRunning   int
	stateBusy      int
	stateReleasing int
}

func (l *actorLedger) counts(now time.Time, rc robotRuntimeConfig) actorLedgerCounts {
	counts := actorLedgerCounts{blocked: l.blockedCount()}
	for _, actor := range l.autoActorPointers() {
		status := actor.status(now, rc)
		counts.auto++
		if status.UID > 0 {
			counts.leased++
		} else {
			counts.idle++
		}
		if status.State == robotActorReleasing {
			counts.releasing++
		}
		switch status.State {
		case robotActorIdle:
			counts.stateIdle++
		case robotActorOffline:
			counts.stateAssigned++
		case robotActorAssigned:
			counts.stateAssigned++
		case robotActorOnline:
			counts.stateOnline++
		case robotActorRunning:
			counts.stateRunning++
		case robotActorBusy:
			counts.stateBusy++
		case robotActorReleasing:
			counts.stateReleasing++
		}
	}
	return counts
}
