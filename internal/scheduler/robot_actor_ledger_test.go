package scheduler

import "testing"

func TestSupervisorStopUIDWithoutRuntimeDoesNotPanic(t *testing.T) {
	s := NewRobotSupervisor(nil, nil)
	registry := newSupervisorActorRegistry(s)
	if registry.StopUID(101, true) {
		t.Fatalf("StopUID should report false when uid is not attached")
	}
}
