package dnf

import "testing"

func TestConnectQueueDeduplicatesUID(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	if !task.enqueueConnect(&RobotVo{UID: 1001}) {
		t.Fatalf("first enqueue should pass")
	}
	if !task.enqueueConnect(&RobotVo{UID: 1001}) {
		t.Fatalf("duplicate enqueue should be treated as already queued")
	}
	if got := len(task.connectQueue); got != 1 {
		t.Fatalf("connect queue got %d entries, want one deduped uid", got)
	}
}
