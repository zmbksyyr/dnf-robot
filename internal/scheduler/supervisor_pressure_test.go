package scheduler

import (
	"sync"
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

type blockingActorStopRuntime struct {
	actorTestRuntime
	started chan int
	release <-chan struct{}
}

func (r *blockingActorStopRuntime) Logout(uid int) robotcap.ActionResult {
	r.started <- uid
	<-r.release
	return robotcap.ActionResult{UID: uid, OK: true}
}

func TestSupervisorPressureReleaseAllowsOnlyOneBatchAndStopWaits(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	supervisorStarted := false
	runtime := &blockingActorStopRuntime{
		started: make(chan int, 2),
		release: release,
	}
	manager := testRobotManagerWithConfig(t, "")
	supervisor := NewRobotSupervisor(manager, runtime)
	defer func() {
		releaseAll()
		if supervisorStarted {
			supervisor.Stop()
		}
	}()
	actors := ensureSupervisorActors(t, supervisor, 2)
	for index, actor := range actors {
		uid := 101 + index
		if !actor.AssignAndWait(uid, time.Second) {
			t.Fatalf("assign actor uid=%d", uid)
		}
		if !supervisor.ledger.TryLeaseUID(uid, actor) {
			t.Fatalf("lease actor uid=%d", uid)
		}
	}

	supervisor.stopSomeAutoActors(true, 1, 0)
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("first pressure release did not start")
	}
	supervisor.stopSomeAutoActors(true, 1, 0)
	if got := supervisor.ledger.Counts(time.Now(), robotconfig.RuntimeConfig{}).Auto; got != 1 {
		t.Fatalf("second pressure release detached another batch: actors=%d want 1", got)
	}

	supervisor.Start()
	supervisorStarted = true
	stopped := make(chan struct{})
	go func() {
		supervisor.Stop()
		close(stopped)
	}()
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("supervisor stop did not start remaining actor release")
	}
	select {
	case <-stopped:
		t.Fatal("supervisor stop returned while pressure release was in flight")
	case <-time.After(20 * time.Millisecond):
	}
	releaseAll()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor stop did not wait for actor releases")
	}

	supervisor.pressureMu.Lock()
	running := supervisor.pressureRunning
	supervisor.pressureMu.Unlock()
	if running {
		t.Fatal("pressure release remained marked in flight after stop")
	}
}
