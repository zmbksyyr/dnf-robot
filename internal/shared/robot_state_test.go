package shared

import "testing"

func TestRuntimeActualState(t *testing.T) {
	tests := []struct {
		name             string
		stateName        string
		disconnectReason int
		missingCore      bool
		want             ActualState
	}{
		{name: "running", stateName: "running", want: ActualRunning},
		{name: "login", stateName: "login", want: ActualLogin},
		{name: "init", stateName: "init", want: ActualConnecting},
		{name: "offline", stateName: "offline", want: ActualStopped},
		{name: "disconnected", stateName: "running", disconnectReason: 8, want: ActualDisconnected},
		{name: "missing core", missingCore: true, want: ActualError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RuntimeActualState(tt.stateName, tt.disconnectReason, tt.missingCore)
			if got != tt.want {
				t.Fatalf("RuntimeActualState() got %q want %q", got, tt.want)
			}
		})
	}
}

func TestDesiredFromOperation(t *testing.T) {
	tests := []struct {
		operation     string
		onlineDesired bool
		want          DesiredState
	}{
		{operation: "cleanup", want: DesiredCleanup},
		{operation: "store", want: DesiredStore},
		{operation: "move", want: DesiredMove},
		{onlineDesired: true, want: DesiredOnline},
		{want: DesiredOffline},
	}
	for _, tt := range tests {
		got := DesiredFromOperation(tt.operation, tt.onlineDesired)
		if got != tt.want {
			t.Fatalf("DesiredFromOperation(%q, %v) got %q want %q", tt.operation, tt.onlineDesired, got, tt.want)
		}
	}
}
