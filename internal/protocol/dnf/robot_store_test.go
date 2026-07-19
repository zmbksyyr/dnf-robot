package dnf

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"
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
