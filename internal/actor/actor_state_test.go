package actor

import (
	"testing"
	"time"
)

func TestOfflineActorKeepsUIDAttached(t *testing.T) {
	a := NewActor(1, ModeAuto, &partyWaitRuntime{})
	a.resetForUID(101)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired || snap.State != StateAssigned {
		t.Fatalf("assigned snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}

	a.setOnlineDesired(false)
	a.tick(time.Now())
	if snap := a.snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.State != StateOffline {
		t.Fatalf("offline snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}

	a.setOnlineDesired(true)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired {
		t.Fatalf("online desired should re-open without detaching uid, uid=%d desired=%v", snap.UID, snap.OnlineDesired)
	}
}

func TestMarkOnlineHealthyClearsHealthWithoutChangingSchedule(t *testing.T) {
	now := time.Now()
	a := NewActor(1, ModeAuto, nil)
	a.resetForUID(101)
	a.failures = 1
	a.firstFailureAt = now.Add(-2 * time.Minute)
	a.lastOnlineTry = now.Add(-2 * time.Minute)
	a.nextStore = now.Add(time.Minute)

	a.markOnlineHealthy()
	snap := a.snapshot()
	if !snap.LastOnlineTry.IsZero() || !snap.FirstFailureAt.IsZero() || snap.Failures != 0 {
		t.Fatalf("healthy actor should clear health state, got last=%s first=%s failures=%d", snap.LastOnlineTry, snap.FirstFailureAt, snap.Failures)
	}
	if !a.nextStore.Equal(now.Add(time.Minute)) {
		t.Fatalf("health reset changed store cooldown: got %s", a.nextStore)
	}
}
