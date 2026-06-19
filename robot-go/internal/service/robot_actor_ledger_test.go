package service

import "testing"

func TestActorLedgerReserveEmptyAutoActor(t *testing.T) {
	ledger := newActorLedger()
	a1 := &robotActor{slotID: 1, mode: robotActorAuto}
	a2 := &robotActor{slotID: 2, mode: robotActorAuto, uid: 202}
	addLedgerActor(t, &ledger, a1)
	addLedgerActor(t, &ledger, a2)

	actor, existing, ok := ledger.reserveEmptyAutoActor(101)
	if !ok || existing || actor != a1 {
		t.Fatalf("reserve got ok=%v existing=%v actor=%v want empty actor 1", ok, existing, actor)
	}
	if got := ledger.actorForUID(101); got != a1 {
		t.Fatalf("leased actor got %v want %v", got, a1)
	}

	actor, existing, ok = ledger.reserveEmptyAutoActor(101)
	if !ok || !existing || actor != a1 {
		t.Fatalf("idempotent reserve got ok=%v existing=%v actor=%v want existing actor 1", ok, existing, actor)
	}

	ledger.blockUID(303)
	if actor, existing, ok := ledger.reserveEmptyAutoActor(303); ok || existing || actor != nil {
		t.Fatalf("blocked reserve got ok=%v existing=%v actor=%v want rejected", ok, existing, actor)
	}
}

func TestActorLedgerDetachUIDsDeduplicatesAndClearsBlocked(t *testing.T) {
	ledger := newActorLedger()
	a1 := &robotActor{slotID: 1, mode: robotActorAuto}
	a2 := &robotActor{slotID: 2, mode: robotActorAuto}
	addLedgerActor(t, &ledger, a1)
	addLedgerActor(t, &ledger, a2)
	ledger.tryLeaseUID(101, a1)
	ledger.tryLeaseUID(102, a2)
	ledger.blockUID(101)

	actors, missing := ledger.detachUIDs([]int{101, 102, 102, 999})
	if len(actors) != 2 {
		t.Fatalf("detached actors got %d want 2", len(actors))
	}
	if len(missing) != 1 || missing[0] != 999 {
		t.Fatalf("missing got %v want [999]", missing)
	}
	if actors, leases := ledgerActorCount(t, &ledger), ledgerLeaseCount(t, &ledger); actors != 0 || leases != 0 {
		t.Fatalf("ledger not empty after detach, actors=%d leases=%d", actors, leases)
	}
	if ledgerIsBlocked(t, &ledger, 101) {
		t.Fatalf("detachUIDs should clear blocked marker for detached uid")
	}
}

func TestActorLedgerFilterBlockedRuntimeStatus(t *testing.T) {
	ledger := newActorLedger()
	ledger.blockUID(101)
	status := map[int]RuntimeRobotStatus{
		101: {UID: 101},
		102: {UID: 102},
	}
	ledger.filterBlockedRuntimeStatus(status)
	if _, ok := status[101]; ok {
		t.Fatalf("blocked uid should be removed from runtime status")
	}
	if _, ok := status[102]; !ok {
		t.Fatalf("unblocked uid should remain in runtime status")
	}
}

func TestActorLedgerDetachSomeAutoActorsHonorsFloor(t *testing.T) {
	ledger := newActorLedger()
	for i := 1; i <= 5; i++ {
		addLedgerActor(t, &ledger, &robotActor{slotID: i, mode: robotActorAuto, uid: 100 + i})
	}
	actors := ledger.detachSomeAutoActors(nil, 4, 3)
	if len(actors) != 2 {
		t.Fatalf("detached actors got %d want 2 due to floor", len(actors))
	}
	if got := ledgerActorCount(t, &ledger); got != 3 {
		t.Fatalf("remaining actors got %d want 3", got)
	}
}

func TestSupervisorStopUIDWithoutRuntimeDoesNotPanic(t *testing.T) {
	s := NewRobotSupervisor(nil, nil)
	registry := newSupervisorActorRegistry(s)
	if registry.StopUID(101, true) {
		t.Fatalf("StopUID should report false when uid is not attached")
	}
}
