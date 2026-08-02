package dnf

import (
	"encoding/binary"
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

func TestSelectCharacUsesCharacterSlotWithoutNarrowingDatabaseCID(t *testing.T) {
	conn := &captureSessionConn{}
	robot := newLoginPacketTestRobot(t, conn)
	robot.CID = 900001
	robot.CharacterSlot = 7

	if !robot.sendSelectCharacUnsafe("database cid regression") {
		t.Fatal("character selection was not sent")
	}
	if len(conn.written) != 29 || binary.LittleEndian.Uint16(conn.written[1:3]) != 4 {
		t.Fatalf("select packet = %x", conn.written)
	}
	payload, err := robot.Cipher.Decrypt(4, conn.written[13:])
	if err != nil {
		t.Fatal(err)
	}
	if payload[0] != robot.CharacterSlot {
		t.Fatalf("selected slot = %d, want %d (database cid=%d)", payload[0], robot.CharacterSlot, robot.CID)
	}
	if robot.CID != 900001 {
		t.Fatalf("database cid changed to %d", robot.CID)
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
