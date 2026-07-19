package actor

import (
	"testing"

	robotcap "robot/internal/capability/robot"
)

func testLedgerActor(slotID int, mode Mode, uid int) *Actor {
	actor := NewActor(slotID, mode, nil)
	if uid > 0 {
		actor.resetForUID(uid)
	}
	return actor
}

func addTestLedgerActor(ledger *Ledger, actor *Actor) {
	ledger.actors[actor.slotIDValue()] = actor
}

func TestLedgerReserveEmptyAutoActor(t *testing.T) {
	ledger := NewLedger()
	a1 := testLedgerActor(1, ModeAuto, 0)
	a2 := testLedgerActor(2, ModeAuto, 202)
	addTestLedgerActor(&ledger, a1)
	addTestLedgerActor(&ledger, a2)

	actor, existing, ok := ledger.ReserveEmptyAutoActor(101)
	if !ok || existing || actor != a1 {
		t.Fatalf("reserve got ok=%v existing=%v actor=%v want empty actor 1", ok, existing, actor)
	}
	if got := ledger.ActorForUID(101); got != a1 {
		t.Fatalf("leased actor got %v want %v", got, a1)
	}

	actor, existing, ok = ledger.ReserveEmptyAutoActor(101)
	if !ok || !existing || actor != a1 {
		t.Fatalf("idempotent reserve got ok=%v existing=%v actor=%v want existing actor 1", ok, existing, actor)
	}

	ledger.BlockUID(303)
	if actor, existing, ok := ledger.ReserveEmptyAutoActor(303); ok || existing || actor != nil {
		t.Fatalf("blocked reserve got ok=%v existing=%v actor=%v want rejected", ok, existing, actor)
	}
}

func TestLedgerDetachUIDsDeduplicatesAndClearsBlocked(t *testing.T) {
	ledger := NewLedger()
	a1 := testLedgerActor(1, ModeAuto, 0)
	a2 := testLedgerActor(2, ModeAuto, 0)
	addTestLedgerActor(&ledger, a1)
	addTestLedgerActor(&ledger, a2)
	ledger.TryLeaseUID(101, a1)
	ledger.TryLeaseUID(102, a2)
	ledger.BlockUID(101)

	actors, missing := ledger.DetachUIDs([]int{101, 102, 102, 999})
	if len(actors) != 2 {
		t.Fatalf("detached actors got %d want 2", len(actors))
	}
	if len(missing) != 1 || missing[0] != 999 {
		t.Fatalf("missing got %v want [999]", missing)
	}
	if actors, leases := len(ledger.actors), len(ledger.uidActors); actors != 0 || leases != 0 {
		t.Fatalf("ledger not empty after detach, actors=%d leases=%d", actors, leases)
	}
	if _, blocked := ledger.blockedUID[101]; blocked {
		t.Fatalf("DetachUIDs should clear blocked marker for detached uid")
	}
}

func TestLedgerFilterBlockedRuntimeStatus(t *testing.T) {
	ledger := NewLedger()
	ledger.BlockUID(101)
	status := map[int]robotcap.RuntimeStatus{
		101: {UID: 101},
		102: {UID: 102},
	}
	ledger.FilterBlockedRuntimeStatus(status)
	if _, ok := status[101]; ok {
		t.Fatalf("blocked uid should be removed from runtime status")
	}
	if _, ok := status[102]; !ok {
		t.Fatalf("unblocked uid should remain in runtime status")
	}
}

func TestLedgerDetachSomeAutoActorsHonorsFloor(t *testing.T) {
	ledger := NewLedger()
	for i := 1; i <= 5; i++ {
		addTestLedgerActor(&ledger, testLedgerActor(i, ModeAuto, 100+i))
	}
	actors := ledger.DetachSomeAutoActors(nil, 4, 3)
	if len(actors) != 2 {
		t.Fatalf("detached actors got %d want 2 due to floor", len(actors))
	}
	if got := len(ledger.actors); got != 3 {
		t.Fatalf("remaining actors got %d want 3", got)
	}
}
