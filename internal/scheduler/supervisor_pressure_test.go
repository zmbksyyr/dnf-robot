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
	var drainingUID int
	select {
	case drainingUID = <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("first pressure release did not start")
	}
	if !supervisor.ledger.HasUID(drainingUID) {
		t.Fatalf("draining uid %d lost its lease before actor exit", drainingUID)
	}
	if supervisor.ledger.TryLeaseUID(drainingUID, actors[0]) {
		t.Fatalf("draining uid %d was leased a second time", drainingUID)
	}
	supervisor.stopSomeAutoActors(true, 1, 0)
	counts := supervisor.ledger.Counts(time.Now(), robotconfig.RuntimeConfig{})
	if counts.Auto != 2 || counts.Draining != 1 {
		t.Fatalf("pressure release must retain draining capacity: actors=%d draining=%d", counts.Auto, counts.Draining)
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

type forceClosingActorRuntime struct {
	blockingActorStopRuntime
	forced chan int
}

func (r *forceClosingActorRuntime) ForceClose(uid int) bool {
	r.forced <- uid
	return true
}

func TestSupervisorShutdownIsBoundedAndKeepsStuckUIDLeased(t *testing.T) {
	release := make(chan struct{})
	runtime := &forceClosingActorRuntime{
		blockingActorStopRuntime: blockingActorStopRuntime{
			started: make(chan int, 1),
			release: release,
		},
		forced: make(chan int, 1),
	}
	manager := testRobotManagerWithConfig(t, "")
	supervisor := NewRobotSupervisor(manager, runtime)
	supervisor.shutdownTimeout = 100 * time.Millisecond
	supervisor.shutdownForceGrace = 40 * time.Millisecond
	actor := ensureSupervisorActors(t, supervisor, 1)[0]
	const uid = 101
	if !actor.AssignAndWait(uid, time.Second) || !supervisor.ledger.TryLeaseUID(uid, actor) {
		t.Fatal("assign shutdown test actor")
	}
	supervisor.Start()

	startedAt := time.Now()
	err := supervisor.StopWithError()
	if err == nil {
		t.Fatal("bounded shutdown should report the stuck actor")
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown exceeded its bound: %s", elapsed)
	}
	select {
	case got := <-runtime.forced:
		if got != uid {
			t.Fatalf("force close uid got %d want %d", got, uid)
		}
	default:
		t.Fatal("shutdown did not attempt runtime force close")
	}
	if !supervisor.ledger.HasUID(uid) || !supervisor.ledger.IsDraining(actor) {
		t.Fatal("stuck actor must retain its uid lease in draining state")
	}
	if supervisor.ledger.TryLeaseUID(uid, actor) {
		t.Fatal("stuck uid was reusable before actor exit")
	}

	close(release)
	select {
	case <-actor.Done():
	case <-time.After(time.Second):
		t.Fatal("actor did not exit after releasing blocked logout")
	}
	if !supervisor.ledger.ReapActor(actor) || supervisor.ledger.HasUID(uid) {
		t.Fatal("completed draining actor was not reaped")
	}
}

func TestSupervisorShutdownBroadcastsBeforeWaiting(t *testing.T) {
	const actorCount = 61
	release := make(chan struct{})
	runtime := &blockingActorStopRuntime{
		started: make(chan int, actorCount),
		release: release,
	}
	manager := testRobotManagerWithConfig(t, "")
	supervisor := NewRobotSupervisor(manager, runtime)
	actors := ensureSupervisorActors(t, supervisor, actorCount)
	for index, actor := range actors {
		uid := 1001 + index
		if !actor.AssignAndWait(uid, time.Second) || !supervisor.ledger.TryLeaseUID(uid, actor) {
			t.Fatalf("assign actor uid=%d", uid)
		}
	}
	supervisor.Start()
	stopped := make(chan error, 1)
	go func() { stopped <- supervisor.StopWithError() }()

	seen := make(map[int]struct{}, actorCount)
	deadline := time.After(2 * time.Second)
	for len(seen) < actorCount {
		select {
		case uid := <-runtime.started:
			seen[uid] = struct{}{}
		case <-deadline:
			t.Fatalf("only %d/%d actors received stop before any logout completed", len(seen), actorCount)
		}
	}
	select {
	case err := <-stopped:
		t.Fatalf("shutdown returned before blocked actors exited: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("shutdown after broadcast: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after releasing all actors")
	}
}
