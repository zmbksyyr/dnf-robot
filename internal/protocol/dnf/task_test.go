package dnf

import (
	"net"
	"testing"
	"time"
)

func TestConnectQueueDeduplicatesUID(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()
	if got := cap(task.connectSlots); got != maxConcurrentConnects {
		t.Fatalf("connect concurrency capacity = %d, want %d", got, maxConcurrentConnects)
	}

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

func TestRobotDnfTaskContainsHandlerPanic(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()
	called := false
	task.keyToHandle["panic-test"] = func(*RobotDnfTask, interface{}) bool {
		panic("bad handler")
	}
	task.handleMessage(MsgQueueData{Type: "panic-test"})
	task.keyToHandle["panic-test"] = func(*RobotDnfTask, interface{}) bool {
		called = true
		return true
	}
	task.handleMessage(MsgQueueData{Type: "panic-test"})
	if !called {
		t.Fatal("task handler remained unusable after panic")
	}
}

func TestMessageDispatchPreservesUIDOrder(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	const count = 100
	got := make([]int, 0, count)
	done := make(chan struct{})
	task.keyToHandle["MsgPublicMsg"] = func(_ *RobotDnfTask, data interface{}) bool {
		msg := data.(*publicMsgInternalData)
		got = append(got, msg.Type)
		if len(got) == count {
			close(done)
		}
		return true
	}

	for i := 0; i < count; i++ {
		if !task.TryAddMessage("MsgPublicMsg", &publicMsgInternalData{ID: 1001, Type: i}) {
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
	shutdownDone := make(chan struct{})
	go func() {
		task.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before the active handler stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wait for the active handler")
	}

	select {
	case <-secondRan:
		t.Fatal("queued message ran after shutdown")
	case <-time.After(100 * time.Millisecond):
	}
	if task.TryAddMessage("MsgMove", &moveInternalData{ID: 1002}) {
		t.Fatal("message was accepted after shutdown")
	}
}

func TestTaskShutdownCancelsConnectContext(t *testing.T) {
	task := NewRobotDnfTask()
	ctx := task.context()
	task.Shutdown()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("shutdown left the connect context active")
	}
}

func TestTaskShutdownClosesAndRemovesRobots(t *testing.T) {
	task := NewRobotDnfTask()
	vo := NewRobotVo(nil)
	vo.Load(UserLoginInfo{UID: 17000001})
	client, server := net.Pipe()
	defer server.Close()
	vo.mu.Lock()
	vo.Controller = task
	vo.Conn = client
	vo.State = StateRun
	vo.publishSnapshotUnsafe()
	vo.mu.Unlock()
	if !task.replaceCurrent(vo.UID, nil, vo) {
		t.Fatal("failed to register robot")
	}

	task.Shutdown()

	if task.Find(int(vo.UID)) != nil {
		t.Fatal("shutdown left robot in registry")
	}
	if snap := vo.Snapshot(); snap.State != StateStop {
		t.Fatalf("robot state = %d, want stopped", snap.State)
	}
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown left robot connection open")
	}
}

func TestTaskShutdownRejectsLateWork(t *testing.T) {
	task := NewRobotDnfTask()
	task.Shutdown()

	vo := NewRobotVo(nil)
	vo.Load(UserLoginInfo{UID: 17000001})
	if task.replaceCurrent(vo.UID, nil, vo) {
		t.Fatal("shutdown task accepted registry replacement")
	}
	if task.enqueueConnect(vo) {
		t.Fatal("shutdown task accepted connect work")
	}
	task.AddMessageDelay("MsgReconnect", vo, 60)
	if got := len(task.messageTimerQueue); got != 0 {
		t.Fatalf("shutdown task retained %d delayed messages", got)
	}
}

func TestLogoutSupersedesQueuedUIDWork(t *testing.T) {
	task := newQueueTestTask()
	const uid = 17000001
	task.TryAddMessage("MsgOnLine", &RobotVo{UID: uid})
	task.TryAddMessage("MsgMove", &moveInternalData{ID: uid, X: 10})
	task.TryAddMessage("MsgPublicMsg", &publicMsgInternalData{ID: uid, Msg: "test"})
	if !task.TryAddMessage("MsgLogout", uid) {
		t.Fatal("logout was rejected")
	}

	queue := task.messageShards[messageShardIndex("MsgLogout", uid)].queue
	if len(queue) != 1 || queue[0].Type != "MsgLogout" || messageUID(queue[0].Type, queue[0].Data) != uid {
		t.Fatalf("queued messages after logout = %+v", queue)
	}
}

func TestOnlineSupersedesQueuedUIDWork(t *testing.T) {
	task := newQueueTestTask()
	const uid = 17000001
	task.TryAddMessage("MsgMove", &moveInternalData{ID: uid, X: 10})
	task.TryAddMessage("MsgPublicMsg", &publicMsgInternalData{ID: uid, Msg: "stale"})
	vo := &RobotVo{UID: uid}
	if !task.TryAddMessage("MsgOnLine", vo) {
		t.Fatal("online was rejected")
	}

	queue := task.messageShards[messageShardIndex("MsgOnLine", vo)].queue
	if len(queue) != 1 || queue[0].Type != "MsgOnLine" || queue[0].Data != vo {
		t.Fatalf("queued messages after online = %+v", queue)
	}
}

func TestMessageQueueNeverEvictsLifecycleWork(t *testing.T) {
	task := newQueueTestTask()
	const uid = 17000001
	shard := task.messageShards[messageShardIndex("MsgMove", &moveInternalData{ID: uid})]
	for i := 0; i < messageShardQueueSize; i++ {
		shard.queue = append(shard.queue, MsgQueueData{Type: "MsgOnLine", Data: &RobotVo{UID: uint32(uid + i)}})
	}
	if task.TryAddMessage("MsgMove", &moveInternalData{ID: uid, X: 10}) {
		t.Fatal("non-lifecycle message displaced protected work")
	}
	if len(shard.queue) != messageShardQueueSize {
		t.Fatalf("queue length = %d", len(shard.queue))
	}
	for _, msg := range shard.queue {
		if msg.Type != "MsgOnLine" {
			t.Fatalf("protected message was replaced by %s", msg.Type)
		}
	}
}

func TestLogoutEvictsMovementWhenQueueIsFull(t *testing.T) {
	task := newQueueTestTask()
	const uid = 17000001
	shard := task.messageShards[messageShardIndex("MsgLogout", uid)]
	for i := 0; i < messageShardQueueSize; i++ {
		shard.queue = append(shard.queue, MsgQueueData{Type: "MsgMove", Data: &moveInternalData{ID: uid + i + 1}})
	}
	if !task.TryAddMessage("MsgLogout", uid) {
		t.Fatal("logout was rejected despite evictable movement")
	}
	if len(shard.queue) != messageShardQueueSize {
		t.Fatalf("queue length = %d", len(shard.queue))
	}
	if got := shard.queue[len(shard.queue)-1]; got.Type != "MsgLogout" || messageUID(got.Type, got.Data) != uid {
		t.Fatalf("queue tail = %+v", got)
	}
}

func TestQueuedMovementCoalescesAfterLifecycleBoundary(t *testing.T) {
	task := newQueueTestTask()
	const uid = 17000001
	task.TryAddMessage("MsgLogout", uid)
	task.TryAddMessage("MsgMove", &moveInternalData{ID: uid, X: 10})
	task.TryAddMessage("MsgMove", &moveInternalData{ID: uid, X: 20})

	queue := task.messageShards[messageShardIndex("MsgMove", &moveInternalData{ID: uid})].queue
	if len(queue) != 2 || queue[0].Type != "MsgLogout" || queue[1].Type != "MsgMove" {
		t.Fatalf("queue = %+v", queue)
	}
	if got := queue[1].Data.(*moveInternalData).X; got != 20 {
		t.Fatalf("coalesced x = %d", got)
	}
}

func newQueueTestTask() *RobotDnfTask {
	task := &RobotDnfTask{done: make(chan struct{})}
	for i := range task.messageShards {
		task.messageShards[i] = newMessageDispatchShard()
	}
	return task
}
