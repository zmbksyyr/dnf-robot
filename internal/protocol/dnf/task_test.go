package dnf

import (
	"testing"
	"time"
)

func TestConnectQueueDeduplicatesUID(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	if !task.enqueueConnect(&RobotVo{UID: 1001}) {
		t.Fatalf("first enqueue should pass")
	}
	if !task.enqueueConnect(&RobotVo{UID: 1001}) {
		t.Fatalf("duplicate enqueue should be treated as already queued")
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(task.connectQueue); got > 1 {
		t.Fatalf("connect queue got %d entries, want at most one deduped uid", got)
	}
}

func TestMessageDispatchPreservesUIDOrder(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	const count = 100
	got := make([]int, 0, count)
	done := make(chan struct{})
	task.keyToHandle["MsgMove"] = func(_ *RobotDnfTask, data interface{}) bool {
		move := data.(*moveInternalData)
		got = append(got, int(move.X))
		if len(got) == count {
			close(done)
		}
		return true
	}

	for i := 0; i < count; i++ {
		if !task.TryAddMessage("MsgMove", &moveInternalData{ID: 1001, X: uint16(i)}) {
			t.Fatalf("enqueue %d failed", i)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ordered messages")
	}

	for i, value := range got {
		if value != i {
			t.Fatalf("message %d handled as %d", i, value)
		}
	}
}

func TestMessageDispatchKeepsAllUIDOperationsOnOneShard(t *testing.T) {
	const uid = 17000001
	want := messageShardIndex("MsgOnLine", &RobotVo{UID: uid})
	cases := []struct {
		typ  string
		data interface{}
	}{
		{"MsgMove", &moveInternalData{ID: uid}},
		{"MsgLogout", uid},
		{"MsgPublicMsg", &publicMsgInternalData{ID: uid}},
		{"MsgOnLineAsyncTaskVec", &RobotVo{UID: uid}},
	}
	for _, tc := range cases {
		if got := messageShardIndex(tc.typ, tc.data); got != want {
			t.Fatalf("%s shard=%d want=%d", tc.typ, got, want)
		}
	}
}

func TestMessageDispatchRunsDifferentUIDsConcurrently(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	firstUID := 1001
	secondUID := firstUID + 1
	for messageShardIndex("MsgMove", &moveInternalData{ID: firstUID}) == messageShardIndex("MsgMove", &moveInternalData{ID: secondUID}) {
		secondUID++
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondDone := make(chan struct{})
	task.keyToHandle["MsgMove"] = func(_ *RobotDnfTask, data interface{}) bool {
		move := data.(*moveInternalData)
		if move.ID == firstUID {
			close(firstStarted)
			<-releaseFirst
		} else if move.ID == secondUID {
			close(secondDone)
		}
		return true
	}

	task.AddMessage("MsgMove", &moveInternalData{ID: firstUID})
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first UID did not start")
	}
	task.AddMessage("MsgMove", &moveInternalData{ID: secondUID})
	select {
	case <-secondDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("second UID was blocked by the first UID")
	}
	close(releaseFirst)
}

func TestMessageDispatchStopsQueuedWorkOnShutdown(t *testing.T) {
	task := NewRobotDnfTask()

	started := make(chan struct{})
	release := make(chan struct{})
	secondRan := make(chan struct{}, 1)
	task.keyToHandle["MsgMove"] = func(_ *RobotDnfTask, data interface{}) bool {
		move := data.(*moveInternalData)
		if move.X == 1 {
			close(started)
			<-release
		} else {
			secondRan <- struct{}{}
		}
		return true
	}
	task.AddMessage("MsgMove", &moveInternalData{ID: 1001, X: 1})
	<-started
	task.AddMessage("MsgMove", &moveInternalData{ID: 1001, X: 2})
	task.Shutdown()
	close(release)

	select {
	case <-secondRan:
		t.Fatal("queued message ran after shutdown")
	case <-time.After(100 * time.Millisecond):
	}
	if task.TryAddMessage("MsgMove", &moveInternalData{ID: 1002}) {
		t.Fatal("message was accepted after shutdown")
	}
}
