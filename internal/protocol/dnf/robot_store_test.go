package dnf

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

var blockingStoreDriverID atomic.Uint64

type blockingStoreDriver struct {
	started  chan struct{}
	release  chan struct{}
	columns  []string
	rows     [][]driver.Value
	queryErr error
}

func (d *blockingStoreDriver) Open(string) (driver.Conn, error) {
	return &blockingStoreConn{driver: d}, nil
}

type blockingStoreConn struct {
	driver *blockingStoreDriver
}

func (*blockingStoreConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported")
}

func (*blockingStoreConn) Close() error { return nil }

func (*blockingStoreConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *blockingStoreConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	select {
	case c.driver.started <- struct{}{}:
	default:
	}
	select {
	case <-c.driver.release:
		if c.driver.queryErr != nil {
			return nil, c.driver.queryErr
		}
		columns := c.driver.columns
		if len(columns) == 0 {
			columns = []string{"item", "price", "count"}
		}
		return &storeTestRows{columns: columns, rows: c.driver.rows}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type storeTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *storeTestRows) Columns() []string { return r.columns }
func (*storeTestRows) Close() error        { return nil }
func (r *storeTestRows) Next(values []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func openStoreTestDB(t *testing.T, drv *blockingStoreDriver) *sql.DB {
	t.Helper()
	driverName := fmt.Sprintf("blocking-store-%d", blockingStoreDriverID.Add(1))
	sql.Register(driverName, drv)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStoreDisplayQueryDoesNotHoldRobotLock(t *testing.T) {
	drv := &blockingStoreDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	db := openStoreTestDB(t, drv)

	r := NewRobotVo(db)
	r.State = StateRun
	r.UID = 17000001
	r.StoreCreated = true
	r.InfanMap[100] = Transaction{ItemId: 100, ItemPos: 3, ItemNum: 1}

	result := make(chan bool, 1)
	go func() {
		result <- r.GetDbDataAndCompleteDisplay()
	}()

	select {
	case <-drv.started:
	case <-time.After(time.Second):
		t.Fatal("store query did not start")
	}
	if _, fresh := r.TrySnapshot(); !fresh {
		t.Fatal("database query held the robot state lock")
	}

	close(drv.release)
	select {
	case ok := <-result:
		if !ok {
			t.Fatal("store display query failed after release")
		}
	case <-time.After(time.Second):
		t.Fatal("store display query did not finish")
	}
}

func TestStoreDisplayQueryDoesNotPublishStaleInventory(t *testing.T) {
	drv := &blockingStoreDriver{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		rows: [][]driver.Value{
			{"100", "50", "1"},
		},
	}
	db := openStoreTestDB(t, drv)
	r := NewRobotVo(db)
	r.State = StateRun
	r.UID = 17000001
	r.StoreCreated = true
	r.InfanMap[100] = Transaction{ItemId: 100, ItemPos: 3, ItemNum: 1}

	result := make(chan bool, 1)
	go func() {
		result <- r.GetDbDataAndCompleteDisplay()
	}()
	select {
	case <-drv.started:
	case <-time.After(time.Second):
		t.Fatal("store query did not start")
	}

	r.mu.Lock()
	r.storeInventoryVersion++
	delete(r.InfanMap, 100)
	r.InfanMap[200] = Transaction{ItemId: 200, ItemPos: 9, ItemNum: 2}
	r.mu.Unlock()
	close(drv.release)

	select {
	case ok := <-result:
		if !ok {
			t.Fatal("store display query failed after release")
		}
	case <-time.After(time.Second):
		t.Fatal("store display query did not finish")
	}
	if r.Snapshot().StoreDisplaySent {
		t.Fatal("stale inventory query published a store display")
	}
}

func TestStoreSendFailureKeepsPacketSequenceAndState(t *testing.T) {
	tests := []struct {
		name          string
		send          func(*RobotVo) bool
		needsCreated  bool
		createFailed  bool
		displayFailed bool
	}{
		{name: "create", send: func(r *RobotVo) bool { return r.CreatePrivateStore() }, createFailed: true},
		{name: "display", send: func(r *RobotVo) bool {
			return r.CompleteDisplay("store", []StoreInfo{{Index: 0, BoxIndex: 1, Price: 10, Count: 1}})
		}, displayFailed: true},
		{name: "item list", send: func(r *RobotVo) bool { return r.GetCompleteDisplay(0) }, needsCreated: true, displayFailed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &failingWriteConn{}
			r := newStorePacketTestRobot(t, conn)
			r.PacketID = 41
			r.StoreCreated = tt.needsCreated

			if tt.send(r) {
				t.Fatal("failed socket write reported success")
			}
			if r.PacketID != 41 {
				t.Fatalf("packet id = %d, want 41", r.PacketID)
			}
			if r.StoreDisplaySent {
				t.Fatal("failed display write marked display as sent")
			}
			if r.IsWaitingItemList {
				t.Fatal("failed item-list write left wait state active")
			}
			if r.StoreCreateRejected != tt.createFailed || r.StoreDisplayRejected != tt.displayFailed {
				t.Fatalf("failure state create=%v display=%v, want create=%v display=%v", r.StoreCreateRejected, r.StoreDisplayRejected, tt.createFailed, tt.displayFailed)
			}
		})
	}
}

func TestStoreDisplayIsSentOnlyOnce(t *testing.T) {
	conn := &captureSessionConn{}
	r := newStorePacketTestRobot(t, conn)
	r.PacketID = 12
	storeInfo := []StoreInfo{{Index: 0, BoxIndex: 1, Price: 10, Count: 1}}

	if !r.CompleteDisplay("store", storeInfo) {
		t.Fatal("first display send failed")
	}
	first := append([]byte(nil), conn.written...)
	if r.CompleteDisplay("store", storeInfo) {
		t.Fatal("duplicate display send reported success")
	}
	if !bytes.Equal(conn.written, first) {
		t.Fatal("duplicate display changed socket output")
	}
	if r.PacketID != 13 {
		t.Fatalf("packet id = %d, want 13", r.PacketID)
	}
}

func TestStoreDisplayAckIgnoresLateRejection(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.RobotTyp = 2
	r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 1, 0))
	if !r.StoreDisplayAck || r.StoreDisplayRejected || r.LastStoreError != 0 {
		t.Fatalf("display ack state ack=%v rejected=%v err=%#x", r.StoreDisplayAck, r.StoreDisplayRejected, r.LastStoreError)
	}

	r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 0, 0x11))
	if !r.StoreDisplayAck || r.StoreDisplayRejected || r.LastStoreError != 0 {
		t.Fatalf("late rejection overwrote ack state ack=%v rejected=%v err=%#x", r.StoreDisplayAck, r.StoreDisplayRejected, r.LastStoreError)
	}
}

func TestStoreDisplayUnknownErrorRejectsImmediately(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.RobotTyp = 2
	r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 0, 0x7f))
	if r.StoreDisplayAck || !r.StoreDisplayRejected || r.LastStoreError != 0x7f {
		t.Fatalf("unknown rejection state ack=%v rejected=%v err=%#x", r.StoreDisplayAck, r.StoreDisplayRejected, r.LastStoreError)
	}
}

func newStorePacketTestRobot(t *testing.T, conn net.Conn) *RobotVo {
	t.Helper()
	r := NewRobotVo(nil)
	r.Cipher = crypt.NewDNFCipher()
	if err := r.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	r.Conn = conn
	r.State = StateRun
	return r
}

func storeReplyPacket(typ uint16, value, storeErr byte) robotInboundPacket {
	raw := make([]byte, 17)
	raw[15] = value
	raw[16] = storeErr
	return robotInboundPacket{data: raw, size: len(raw), flag: 1, typ: typ, isAnti: true}
}
