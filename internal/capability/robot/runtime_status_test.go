package robot

import "testing"

func TestStateName(t *testing.T) {
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
		if got := StateName(state); got != want {
			t.Fatalf("state %d: got %q want %q", state, got, want)
		}
	}
}

func TestSummarizeRuntimeStatus(t *testing.T) {
	summary := SummarizeRuntimeStatusSlice([]RuntimeStatus{
		{StateName: "running", DisconnectReason: 0},
		{StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		{StateName: "running", DisconnectReason: 1, RobotType: 2, StoreDisplayAck: true},
		{StateName: "init", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 2},
		{StateName: "stop", DisconnectReason: 0},
	})
	if summary.Running != 2 {
		t.Fatalf("running got %d want 2", summary.Running)
	}
	if summary.Connecting != 2 {
		t.Fatalf("connecting got %d want 2", summary.Connecting)
	}
	if summary.Stores != 1 {
		t.Fatalf("stores got %d want 1", summary.Stores)
	}
}

func TestActiveRuntimeStatusRequiresNoDisconnect(t *testing.T) {
	tests := []struct {
		name string
		st   RuntimeStatus
		want bool
	}{
		{name: "running", st: RuntimeStatus{StateName: "running", DisconnectReason: 0}, want: true},
		{name: "running disconnected", st: RuntimeStatus{StateName: "running", DisconnectReason: 8}, want: false},
		{name: "login", st: RuntimeStatus{StateName: "login", DisconnectReason: 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ActiveRuntimeStatus(tt.st); got != tt.want {
				t.Fatalf("ActiveRuntimeStatus() got %v want %v", got, tt.want)
			}
		})
	}
}
