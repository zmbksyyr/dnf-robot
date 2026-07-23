package dnf

import (
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

func TestSelectCharacIsDelayedAfterNCC(t *testing.T) {
	resetLoginSelectGate()
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)
	robot.NccSent = true

	robot.mu.Lock()
	robot.scheduleSelectCharacUnsafe(20 * time.Millisecond)
	queued := robot.SelectCharacQueued
	sent := robot.SelectCharacSent
	robot.mu.Unlock()
	if !queued || sent {
		t.Fatalf("queued=%t sent=%t immediately after scheduling", queued, sent)
	}
	waitForSelectCharac(t, robot)
	if len(conn.written) == 0 {
		t.Fatal("character selection packet was not sent")
	}
}

func TestSelectCharacGateSerializesSessions(t *testing.T) {
	resetLoginSelectGate()
	first := reserveLoginSelectDelay(10 * time.Millisecond)
	second := reserveLoginSelectDelay(10 * time.Millisecond)
	if first < 0 || second-first < loginSelectInterval-20*time.Millisecond {
		t.Fatalf("gate delays first=%s second=%s interval=%s", first, second, second-first)
	}
}

func TestSelectCharacScheduleIsIdempotent(t *testing.T) {
	resetLoginSelectGate()
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)
	robot.NccSent = true

	robot.mu.Lock()
	robot.scheduleSelectCharacUnsafe(time.Millisecond)
	robot.scheduleSelectCharacUnsafe(time.Millisecond)
	robot.mu.Unlock()
	waitForSelectCharac(t, robot)
	written := len(conn.written)
	time.Sleep(20 * time.Millisecond)
	if len(conn.written) != written {
		t.Fatal("duplicate character selection packet was sent")
	}
}

func TestSelectCharacScheduleStopsWithLogin(t *testing.T) {
	resetLoginSelectGate()
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)
	robot.NccSent = true

	robot.mu.Lock()
	robot.scheduleSelectCharacUnsafe(time.Millisecond)
	robot.State = StateStop
	robot.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if robot.SelectCharacSent || len(conn.written) != 0 {
		t.Fatal("stale schedule selected a character after login stopped")
	}
}

func newLoginPacketTestRobot(t *testing.T, conn *captureSessionConn) *RobotVo {
	t.Helper()
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateLogin
	return robot
}

func waitForSelectCharac(t *testing.T, robot *RobotVo) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		robot.mu.Lock()
		sent := robot.SelectCharacSent
		robot.mu.Unlock()
		if sent {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("character selection was not sent")
}

func resetLoginSelectGate() {
	loginSelectGate.Lock()
	loginSelectGate.next = time.Time{}
	loginSelectGate.Unlock()
}
