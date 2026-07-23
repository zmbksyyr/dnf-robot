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
	if delay := claimLoginSelectSlot(); delay != 0 {
		t.Fatalf("first gate claim delay=%s", delay)
	}
	if delay := claimLoginSelectSlot(); delay < loginSelectInterval-20*time.Millisecond {
		t.Fatalf("second gate claim delay=%s interval=%s", delay, loginSelectInterval)
	}
}

func TestSelectCharacGateDoesNotReserveFutureSlots(t *testing.T) {
	resetLoginSelectGate()
	if delay := claimLoginSelectSlot(); delay != 0 {
		t.Fatalf("first gate claim delay=%s", delay)
	}
	for i := 0; i < 100; i++ {
		_ = claimLoginSelectSlot()
	}
	loginSelectGate.Lock()
	reservedUntil := loginSelectGate.next
	loginSelectGate.Unlock()
	if remaining := time.Until(reservedUntil); remaining > loginSelectInterval+20*time.Millisecond {
		t.Fatalf("gate accumulated abandoned reservations: %s", remaining)
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
