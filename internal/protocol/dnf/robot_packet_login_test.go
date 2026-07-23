package dnf

import (
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

func TestSelectCharacWaitsForNCCAndCharacList(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateLogin

	robot.NccSent = true
	if robot.trySelectCharacUnsafe("ncc only") {
		t.Fatal("selected character before character list was ready")
	}
	robot.NccSent = false
	robot.CharacListReady = true
	if robot.trySelectCharacUnsafe("character list only") {
		t.Fatal("selected character before NCC completed")
	}
	robot.NccSent = true
	if !robot.trySelectCharacUnsafe("both ready") {
		t.Fatal("did not select character after both login prerequisites completed")
	}
	if !robot.SelectCharacSent || len(conn.written) == 0 {
		t.Fatal("character selection packet was not sent")
	}
}

func TestSelectCharacIsSentOnlyOnce(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateLogin
	robot.NccSent = true
	robot.CharacListReady = true

	if !robot.trySelectCharacUnsafe("first") {
		t.Fatal("first character selection was not sent")
	}
	written := len(conn.written)
	if robot.trySelectCharacUnsafe("duplicate") {
		t.Fatal("duplicate character selection was accepted")
	}
	if len(conn.written) != written {
		t.Fatal("duplicate character selection wrote another packet")
	}
}

func TestSelectCharacFallbackSupportsServersWithoutType53(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateLogin
	robot.NccSent = true

	robot.mu.Lock()
	robot.scheduleSelectCharacFallbackAfterUnsafe(time.Millisecond)
	robot.mu.Unlock()
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
	t.Fatal("fallback did not select character without type=53")
}

func TestSelectCharacFallbackDoesNotSendAfterLoginStops(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateLogin
	robot.NccSent = true

	robot.mu.Lock()
	robot.scheduleSelectCharacFallbackAfterUnsafe(time.Millisecond)
	robot.State = StateStop
	robot.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if robot.SelectCharacSent || len(conn.written) != 0 {
		t.Fatal("stale fallback selected a character after login stopped")
	}
}
