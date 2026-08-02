package dnf

import (
	"errors"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type panicReadConn struct {
	closed atomic.Bool
}

func (*panicReadConn) Read([]byte) (int, error)         { panic("read failure") }
func (*panicReadConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *panicReadConn) Close() error                   { c.closed.Store(true); return nil }
func (*panicReadConn) LocalAddr() net.Addr              { return nil }
func (*panicReadConn) RemoteAddr() net.Addr             { return nil }
func (*panicReadConn) SetDeadline(time.Time) error      { return errors.ErrUnsupported }
func (*panicReadConn) SetReadDeadline(time.Time) error  { return errors.ErrUnsupported }
func (*panicReadConn) SetWriteDeadline(time.Time) error { return errors.ErrUnsupported }

func TestRobotTryCloseOutReturnsImmediatelyWhenSessionIsBusy(t *testing.T) {
	robot := NewRobotVo(nil)
	robot.mu.Lock()
	before := runtime.NumGoroutine()
	startedAt := time.Now()
	for attempt := 0; attempt < 1000; attempt++ {
		if robot.TryCloseOut() {
			robot.mu.Unlock()
			t.Fatal("TryCloseOut acquired an already-held session lock")
		}
	}
	elapsed := time.Since(startedAt)
	after := runtime.NumGoroutine()
	robot.mu.Unlock()

	if elapsed > 100*time.Millisecond {
		t.Fatalf("busy TryCloseOut calls blocked for %s", elapsed)
	}
	if after > before {
		t.Fatalf("busy TryCloseOut created goroutines: before=%d after=%d", before, after)
	}
	if !robot.TryCloseOut() {
		t.Fatal("TryCloseOut should close an unlocked session")
	}
	if snapshot := robot.Snapshot(); snapshot.State != StateStop {
		t.Fatalf("closed session state got %d want %d", snapshot.State, StateStop)
	}
}

func TestRobotReadLoopPanicClosesAndUnregistersSession(t *testing.T) {
	task := NewRobotDnfTask()
	defer task.Shutdown()

	robot := NewRobotVo(nil)
	robot.Load(UserLoginInfo{UID: 17000001})
	if !robot.prepareConnect(task) || !task.replaceCurrent(robot.UID, nil, robot) {
		t.Fatal("failed to register robot")
	}
	conn := &panicReadConn{}
	robot.mu.Lock()
	robot.Conn = conn
	robot.State = StateLogin
	robot.publishSnapshotUnsafe()
	robot.mu.Unlock()

	robot.readLoop(conn)

	if !conn.closed.Load() {
		t.Fatal("panicking read connection was not closed")
	}
	if task.Find(int(robot.UID)) != nil {
		t.Fatal("panicking session remained registered")
	}
	if snapshot := robot.Snapshot(); snapshot.State != StateStop {
		t.Fatalf("snapshot state got %d want %d", snapshot.State, StateStop)
	}
	robot.mu.Lock()
	defer robot.mu.Unlock()
	if robot.Conn != nil {
		t.Fatal("panicking session retained its connection")
	}
}
