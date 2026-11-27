package dnf

import (
	"net"
	"testing"
	"time"
)

func TestConnectClosesDialResultAfterLogout(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	vo := NewRobotVo(nil)
	vo.Load(UserLoginInfo{IP: "192.0.2.1", Port: 10011, UID: 17000001})
	if !vo.prepareConnect(task) || !task.replaceCurrent(vo.UID, nil, vo) {
		t.Fatal("failed to register pending robot")
	}

	client, server := net.Pipe()
	defer server.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		vo.connect(func(string, int, string) (net.Conn, error) {
			close(started)
			<-release
			return client, nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dial did not start")
	}
	vo.CloseOut()
	if !task.DeleteIf(vo.UID, vo) {
		t.Fatal("pending robot was not removed")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connect did not finish after logout")
	}

	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("stale dial result remained open")
	}
	if snap := vo.Snapshot(); snap.State != StateStop {
		t.Fatalf("state = %d, want stopped", snap.State)
	}
	if task.Find(int(vo.UID)) != nil {
		t.Fatal("logged out robot returned to registry")
	}
}

func TestLogoutCancelsPendingReconnect(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	old := NewRobotVo(nil)
	old.Load(UserLoginInfo{
		IP:        "192.0.2.1",
		Port:      10011,
		UID:       17000001,
		MaxReConn: 3,
		ReDelay:   60_000,
	})
	if !old.prepareConnect(task) || !task.replaceCurrent(old.UID, nil, old) {
		t.Fatal("failed to register robot")
	}
	if !old.RefishConnect() {
		t.Fatal("reconnect was not prepared")
	}

	pending := task.Find(int(old.UID))
	if pending == nil || pending == old {
		t.Fatal("reconnect did not install a pending replacement")
	}
	if snap := pending.Snapshot(); snap.State != StateInit {
		t.Fatalf("pending state = %d, want init", snap.State)
	}

	task.msgLogout(task, int(old.UID))
	if task.Find(int(old.UID)) != nil {
		t.Fatal("logout did not remove pending reconnect")
	}
	if task.msgReconnect(task, pending) {
		t.Fatal("logged out reconnect was accepted")
	}
	if snap := pending.Snapshot(); snap.State != StateStop {
		t.Fatalf("pending state after logout = %d, want stopped", snap.State)
	}
}

func TestInventoryRejectionRemainsVisibleWithoutReconnect(t *testing.T) {
	for _, checkState := range []bool{false, true} {
		t.Run(map[bool]string{false: "reconnect", true: "state_check"}[checkState], func(t *testing.T) {
			task := NewRobotDnfTask()
			defer task.Shutdown()
			vo := NewRobotVo(nil)
			vo.Load(UserLoginInfo{UID: 17000001, MaxReConn: 3, ReDelay: 60000})
			if !vo.prepareConnect(task) || !task.replaceCurrent(vo.UID, nil, vo) {
				t.Fatal("register rejected session")
			}
			vo.mu.Lock()
			vo.State = StateLogin
			vo.DisconReason = DisconnectReason(36)
			vo.Conn = &captureSessionConn{}
			vo.mu.Unlock()
			if checkState {
				vo.CheckUserState()
			} else {
				vo.RefishConnect()
			}
			if task.Find(int(vo.UID)) != vo {
				t.Fatal("rejection was removed or replaced by an automatic retry")
			}
			if snap := vo.Snapshot(); snap.State != StateWrong || snap.DisconnectReason != 36 || snap.Reconnects != 0 {
				t.Fatalf("rejected snapshot = %+v", snap)
			}
			if vo.Conn != nil || vo.CheckUserState() || vo.RefishConnect() || task.msgReconnect(task, vo) {
				t.Fatal("rejected session retained a connection or accepted reconnect")
			}
			if task.Find(int(vo.UID)) != vo {
				t.Fatal("a repeated state check lost the rejection")
			}
			task.msgLogout(task, int(vo.UID))
			if task.Find(int(vo.UID)) != nil || vo.Snapshot().State != StateStop {
				t.Fatal("explicit logout did not clear rejection")
			}
			vo.RefishConnect()
			if task.Find(int(vo.UID)) != nil || vo.Snapshot().State != StateStop {
				t.Fatal("late reconnect resurrected a logged-out rejection")
			}
		})
	}
}

func TestOtherDisconnectReasonsRetainReconnectPolicy(t *testing.T) {
	for _, reason := range []DisconnectReason{NoDisconnect, BadTokenOrCid, BadUid, RepeatLogin, PunishReason, 99} {
		task := NewRobotDnfTask()
		vo := NewRobotVo(nil)
		vo.Load(UserLoginInfo{UID: 17000001, MaxReConn: 3, ReDelay: 60000})
		if !vo.prepareConnect(task) || !task.replaceCurrent(vo.UID, nil, vo) {
			task.Shutdown()
			t.Fatal("register reconnecting session")
		}
		vo.mu.Lock()
		vo.DisconReason = reason
		vo.mu.Unlock()
		vo.RefishConnect()
		pending := task.Find(int(vo.UID))
		if pending == nil || pending == vo || pending.Snapshot().State != StateInit || pending.Snapshot().Reconnects != 1 {
			task.Shutdown()
			t.Fatalf("reason %d changed reconnect policy", reason)
		}
		task.Shutdown()
	}
}
