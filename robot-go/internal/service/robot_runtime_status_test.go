package service

import (
	"testing"
)

func TestRobotStateName(t *testing.T) {
	tests := map[int]string{
		0:  "stop",
		1:  "init",
		2:  "login",
		3:  "running",
		4:  "clean",
		5:  "wrong",
		99: "unknown",
	}
	for state, want := range tests {
		if got := robotStateName(state); got != want {
			t.Fatalf("state %d: got %q want %q", state, got, want)
		}
	}
}

func TestSummarizeRuntimeStatus(t *testing.T) {
	running, connecting, stores := summarizeRuntimeStatusSlice([]RuntimeRobotStatus{
		{StateName: "running", DisconnectReason: 0},
		{StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		{StateName: "running", DisconnectReason: 1, RobotType: 2, StoreDisplayAck: true},
		{StateName: "init", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 2},
		{StateName: "stop", DisconnectReason: 0},
	})
	if running != 2 {
		t.Fatalf("running got %d want 2", running)
	}
	if connecting != 2 {
		t.Fatalf("connecting got %d want 2", connecting)
	}
	if stores != 1 {
		t.Fatalf("stores got %d want 1", stores)
	}
}

func TestActiveRuntimeStatusRequiresNoDisconnect(t *testing.T) {
	tests := []struct {
		name string
		st   RuntimeRobotStatus
		want bool
	}{
		{name: "running", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 0}, want: true},
		{name: "running disconnected", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 8}, want: false},
		{name: "login", st: RuntimeRobotStatus{StateName: "login", DisconnectReason: 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activeRuntimeStatus(tt.st); got != tt.want {
				t.Fatalf("activeRuntimeStatus() got %v want %v", got, tt.want)
			}
		})
	}
}
