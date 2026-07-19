package dnf

import (
	"runtime"
	"testing"
	"time"
)

func TestRobotTryCloseOutReturnsImmediatelyWhenSessionIsBusy(t *testing.T) {
	robot := NewRobotVo(nil)
	robot.mu.Lock()
	before := runtime.NumGoroutine()
	startedAt := time.Now()
	for attempt := 0; attempt < 1000; attempt++ {
		if robot.TryCloseOut() {
			robot.mu.Unlock()
			t.Fatal("TryCloseOut acquired an already-held session lock")
		}
	}
	elapsed := time.Since(startedAt)
	after := runtime.NumGoroutine()
	robot.mu.Unlock()

	if elapsed > 100*time.Millisecond {
		t.Fatalf("busy TryCloseOut calls blocked for %s", elapsed)
	}
	if after > before {
		t.Fatalf("busy TryCloseOut created goroutines: before=%d after=%d", before, after)
	}
	if !robot.TryCloseOut() {
		t.Fatal("TryCloseOut should close an unlocked session")
	}
	if snapshot := robot.Snapshot(); snapshot.State != StateStop {
		t.Fatalf("closed session state got %d want %d", snapshot.State, StateStop)
	}
}
