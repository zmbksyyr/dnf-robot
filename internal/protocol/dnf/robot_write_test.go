package dnf

import (
	"errors"
	"net"
	"testing"
	"time"
)

type failingWriteConn struct {
	deadline time.Time
	closed   bool
}

func (*failingWriteConn) Read([]byte) (int, error)             { return 0, errors.New("unused") }
func (*failingWriteConn) Write([]byte) (int, error)            { return 0, errors.New("write failed") }
func (c *failingWriteConn) Close() error                       { c.closed = true; return nil }
func (*failingWriteConn) LocalAddr() net.Addr                  { return testNetAddr("local") }
func (*failingWriteConn) RemoteAddr() net.Addr                 { return testNetAddr("remote") }
func (*failingWriteConn) SetDeadline(time.Time) error          { return nil }
func (*failingWriteConn) SetReadDeadline(time.Time) error      { return nil }
func (c *failingWriteConn) SetWriteDeadline(t time.Time) error { c.deadline = t; return nil }

type testNetAddr string

func (a testNetAddr) Network() string { return string(a) }
func (a testNetAddr) String() string  { return string(a) }

func TestSendRawBoundsWriteAndClosesFailedConnection(t *testing.T) {
	conn := &failingWriteConn{}
	vo := NewRobotVo(nil)
	vo.UID = 17000001
	vo.Conn = conn
	started := time.Now()
	if vo.sendRaw([]byte{1}) {
		t.Fatal("failed write reported success")
	}
	if !conn.closed {
		t.Fatal("failed connection was not closed")
	}
	want := started.Add(robotSocketWriteTimeout)
	if conn.deadline.Before(want.Add(-100*time.Millisecond)) || conn.deadline.After(want.Add(500*time.Millisecond)) {
		t.Fatalf("write deadline = %s, want near %s", conn.deadline, want)
	}
}
