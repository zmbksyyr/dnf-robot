package dnf

import (
	"errors"
	"testing"

	"robot/internal/protocol/dnf/crypt"
)

func TestSelectCharacIsSentOnlyOnce(t *testing.T) {
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)

	if !robot.sendSelectCharacUnsafe("first trigger") {
		t.Fatal("first character selection was not sent")
	}
	written := len(conn.written)
	if robot.sendSelectCharacUnsafe("second trigger") {
		t.Fatal("duplicate character selection was accepted")
	}
	if len(conn.written) != written {
		t.Fatal("duplicate character selection packet was written")
	}
}

func TestSelectCharacSendFailureDoesNotMarkSent(t *testing.T) {
	conn := &captureSessionConn{writeErr: errors.New("write failed")}
	robot := newLoginPacketTestRobot(t, conn)

	if robot.sendSelectCharacUnsafe("failed trigger") {
		t.Fatal("failed character selection reported success")
	}
	if robot.SelectCharacSent {
		t.Fatal("failed character selection was marked sent")
	}
}

func TestSelectCharacRequiresLoginState(t *testing.T) {
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)
	robot.State = StateStop

	if robot.sendSelectCharacUnsafe("stopped login") {
		t.Fatal("stopped login selected a character")
	}
	if len(conn.written) != 0 {
		t.Fatal("stopped login wrote a character selection packet")
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
