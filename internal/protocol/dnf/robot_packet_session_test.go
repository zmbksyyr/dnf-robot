package dnf

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

type captureSessionConn struct {
	written  []byte
	writeErr error
}

func (*captureSessionConn) Read([]byte) (int, error) { return 0, nil }
func (c *captureSessionConn) Write(data []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	c.written = append(c.written, data...)
	return len(data), nil
}
func (*captureSessionConn) Close() error                     { return nil }
func (*captureSessionConn) LocalAddr() net.Addr              { return testNetAddr("local") }
func (*captureSessionConn) RemoteAddr() net.Addr             { return testNetAddr("remote") }
func (*captureSessionConn) SetDeadline(time.Time) error      { return nil }
func (*captureSessionConn) SetReadDeadline(time.Time) error  { return nil }
func (*captureSessionConn) SetWriteDeadline(time.Time) error { return nil }

func TestCheckConnectionPacketSendsChecksumResponse(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateRun
	robot.PacketID = 77

	want, err := buildSendPacket(0, 77, make([]byte, 8), robot.Cipher)
	if err != nil {
		t.Fatal(err)
	}
	robot.handleSessionPacketUnsafe(robotInboundPacket{flag: 0, typ: 0, size: 15, data: make([]byte, 15)})

	if !bytes.Equal(conn.written, want) {
		t.Fatalf("check connection response = %x, want %x", conn.written, want)
	}
	if len(conn.written) != 21 || conn.written[0] != 1 || conn.written[1] != 0 || conn.written[2] != 0 {
		t.Fatalf("check connection response shape = %x", conn.written)
	}
	if robot.PacketID != 78 {
		t.Fatalf("packet id = %d, want 78", robot.PacketID)
	}
}

func TestCheckConnectionPacketKeepsSequenceWhenSendFails(t *testing.T) {
	conn := &captureSessionConn{writeErr: errors.New("write failed")}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateRun
	robot.PacketID = 77

	robot.handleSessionPacketUnsafe(robotInboundPacket{flag: 0, typ: 0, size: 15, data: make([]byte, 15)})

	if robot.PacketID != 77 {
		t.Fatalf("packet id = %d, want 77 after failed send", robot.PacketID)
	}
}
