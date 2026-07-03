package actor

import (
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
)

func TestEvaluateStatusFailureCount(t *testing.T) {
	now := time.Now()
	status := EvaluateStatus(Snapshot{
		Mode:           ModeAuto,
		UID:            801,
		Failures:       5,
		FirstFailureAt: now.Add(-10 * time.Second),
	}, now, StatusConfig{BadFailures: 5}, nil)
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}
}

func TestEvaluateStatusBusyDoesNotRecycle(t *testing.T) {
	status := EvaluateStatus(Snapshot{
		Mode:     ModeAuto,
		UID:      801,
		Busy:     true,
		BusyKind: "store",
		Failures: 5,
	}, time.Now(), StatusConfig{BadFailures: 5}, nil)
	if status.RecycleUID || status.Health != HealthBusy {
		t.Fatalf("busy status got recycle=%v health=%s, want busy without recycle", status.RecycleUID, status.Health)
	}
}

func TestEvaluateStatusOnlineTimeout(t *testing.T) {
	now := time.Now()
	status := EvaluateStatus(Snapshot{
		Mode:          ModeAuto,
		UID:           1001,
		State:         StateOnline,
		LastOnlineTry: now.Add(-61 * time.Second),
	}, now, StatusConfig{BadFailures: 3, OnlineConfirmTimeoutMS: 60000}, func(uid int) (robotcap.RuntimeStatus, bool) {
		return robotcap.RuntimeStatus{}, false
	})
	if status.RecycleUID || status.HealthReason != "online_confirm_timeout" {
		t.Fatalf("timeout status got recycle=%v reason=%q, want timeout without recycle", status.RecycleUID, status.HealthReason)
	}
}

func TestSnapshotDerivedDisplayState(t *testing.T) {
	snap := Snapshot{State: StateOffline, OnlineDesired: false}
	if got := Operation(snap); got != "offline" {
		t.Fatalf("offline operation got %q", got)
	}
	snap = Snapshot{State: StateBusy, BusyKind: "store", OnlineDesired: true}
	if got := Operation(snap); got != "store" {
		t.Fatalf("busy operation got %q", got)
	}
	if got := HealthState("ok", Snapshot{Failures: 1}); got != "suspect" {
		t.Fatalf("health got %q want suspect", got)
	}
}

func TestStopPriority(t *testing.T) {
	status := map[int]robotcap.RuntimeStatus{
		1: {UID: 1, StateName: "running", DisconnectReason: 0},
		2: {UID: 2, StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		3: {UID: 3, StateName: "login", DisconnectReason: 0},
	}
	tests := []struct {
		uid  int
		want int
	}{
		{uid: 0, want: 0},
		{uid: 99, want: 1},
		{uid: 3, want: 1},
		{uid: 2, want: 2},
		{uid: 1, want: 3},
	}
	for _, tt := range tests {
		if got := StopPriority(tt.uid, status); got != tt.want {
			t.Fatalf("uid %d priority got %d want %d", tt.uid, got, tt.want)
		}
	}
}
