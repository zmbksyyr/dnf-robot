package scheduler

import (
	"sync"
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
)

type blockedLogoutDoll struct {
	noopRuntime
	started chan int
	release chan struct{}
}

func (d *blockedLogoutDoll) Logout(uid int) error {
	d.started <- uid
	<-d.release
	return nil
}

func (d *blockedLogoutDoll) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	// Keep connections visible so the existing confirmation returns Pending.
	// The concurrency limit must neither report closure nor invalidate caches.
	status := make(map[int]robotcap.RuntimeStatus)
	for uid := 1; uid <= actorLogoutConcurrency*2+1; uid++ {
		status[uid] = robotcap.RuntimeStatus{UID: uid, StateName: robotcap.RuntimeStateRunning}
	}
	return status
}

func TestActorLogoutBoundsConcurrencyAndPreservesPendingResult(t *testing.T) {
	const count = actorLogoutConcurrency*2 + 1
	doll := &blockedLogoutDoll{started: make(chan int, count), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(doll.release) }) })
	runtime := NewRobotRuntime(NewRobotManager(nil, nil, doll))
	results := make(chan robotcap.ActionResult, count)
	for uid := 1; uid <= count; uid++ {
		go func(uid int) { results <- runtime.Logout(uid) }(uid)
	}
	deadline := time.After(3 * time.Second)
	for i := 0; i < actorLogoutConcurrency; i++ {
		select {
		case <-doll.started:
		case <-deadline:
			t.Fatal("first logout wave did not reach the transport")
		}
	}
	select {
	case uid := <-doll.started:
		t.Fatalf("uid %d exceeded logout concurrency before confirmation", uid)
	case <-time.After(100 * time.Millisecond):
	}
	release.Do(func() { close(doll.release) })
	seen := make(map[int]bool, count)
	for i := 0; i < count; i++ {
		select {
		case result := <-results:
			if result.OK || result.State != robotcap.ActionStatePending || seen[result.UID] {
				t.Fatalf("logout lost pending state or repeated UID: %+v", result)
			}
			seen[result.UID] = true
		case <-deadline:
			t.Fatal("queued logouts did not all finish")
		}
	}
	if len(doll.started) != count-actorLogoutConcurrency || len(runtime.logoutSlots) != 0 {
		t.Fatal("logout transport calls were skipped or a slot leaked")
	}
	for uid := range seen {
		if runtime.uidLocks.Active(uid) {
			t.Fatalf("logout retained UID lock %d", uid)
		}
	}
}
