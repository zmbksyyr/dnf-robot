package scheduler

import (
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

type batchStoreRuntime struct {
	actorTestRuntime
	entered chan int
	release chan struct{}
}

func (r *batchStoreRuntime) Store(uid int) robotcap.ActionResult {
	r.entered <- uid
	<-r.release
	if uid == 102 {
		return robotcap.ActionResult{UID: uid, State: robotcap.ActionStateNotConfirmed, Message: "store_err_0x3e"}
	}
	return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateStore}
}

func TestManualStoreBatchBoundsConcurrentActorsAndRetainsResults(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	runtime := &batchStoreRuntime{entered: make(chan int, 6), release: make(chan struct{}, 6)}
	supervisor := NewRobotSupervisor(m, runtime)
	registry := newSupervisorActorRegistry(supervisor)
	var robots []robotcap.Info
	for uid := 101; uid <= 106; uid++ {
		if !registry.AttachUID(uid, time.Second) {
			t.Fatalf("attach uid=%d", uid)
		}
		robots = append(robots, robotcap.Info{UID: uid, CID: uid + 1000})
	}
	t.Cleanup(func() { close(runtime.release); supervisor.stopAll() })
	done := make(chan robotcap.CommandResult, 1)
	go func() {
		done <- m.storeActors(registry, robots, robotconfig.RuntimeConfig{
			SchedulerStoreConcurrent: 2, SystemManualActionTimeoutSec: 5,
		})
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-runtime.entered:
		case <-time.After(time.Second):
			t.Fatal("independent store actors are still serialized")
		}
	}
	select {
	case uid := <-runtime.entered:
		t.Fatalf("uid=%d exceeded the two-actor limit", uid)
	case <-time.After(50 * time.Millisecond):
	}
	for i := 2; i < len(robots); i++ {
		runtime.release <- struct{}{}
		select {
		case <-runtime.entered:
		case <-time.After(time.Second):
			t.Fatal("completed store did not release its slot")
		}
	}
	runtime.release <- struct{}{}
	runtime.release <- struct{}{}
	select {
	case result := <-done:
		if result.Requested != 6 || result.Accepted != 5 || result.Confirmed != 5 || result.Failed != 1 || len(result.Robots) != 6 {
			t.Fatalf("result=%+v", result)
		}
		for i, item := range result.Robots {
			if item.UID != robots[i].UID || item.CID != robots[i].CID {
				t.Fatalf("result order/identity changed: %+v", result.Robots)
			}
		}
		if result.Robots[1].OK || result.Robots[1].Message != "store_err_0x3e" {
			t.Fatalf("server rejection was lost: %+v", result.Robots[1])
		}
	case <-time.After(time.Second):
		t.Fatal("batch did not finish after all actors replied")
	}
}

func TestManualStoreBatchReportsUnstartedActorsDuringShutdown(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.shuttingDown = true
	robots := []robotcap.Info{{UID: 101, CID: 1001}, {UID: 102, CID: 1002}}
	// No registry is needed: shutdown must not enqueue new work.
	result := m.storeActors(nil, robots, robotconfig.RuntimeConfig{SchedulerStoreConcurrent: 1})
	if result.Requested != 2 || result.Accepted != 0 || result.Confirmed != 0 || result.Failed != 2 || len(result.Robots) != 2 {
		t.Fatalf("result=%+v", result)
	}
	for i, item := range result.Robots {
		if item.UID != robots[i].UID || item.CID != robots[i].CID || item.OK || item.State != robotcap.ActionStateCancelled {
			t.Fatalf("shutdown result=%+v", item)
		}
	}
}
