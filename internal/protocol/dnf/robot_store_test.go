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
	started chan struct{}
	release chan struct{}
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
		return emptyStoreRows{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type emptyStoreRows struct{}

func (emptyStoreRows) Columns() []string         { return []string{"item", "price", "count"} }
func (emptyStoreRows) Close() error              { return nil }
func (emptyStoreRows) Next([]driver.Value) error { return io.EOF }

func TestCalculateTradeQuoteUsesActiveValidTransactions(t *testing.T) {
	snapshot := tradeQuoteSnapshot{}
	snapshot.transactions[0] = Transaction{ItemId: 100, ItemNum: 3}
	snapshot.active[0] = true
	snapshot.transactions[1] = Transaction{ItemId: 200, ItemNum: 2}
	snapshot.active[1] = true
	snapshot.transactions[2] = Transaction{ItemId: 300, ItemNum: 7}
	snapshot.active[2] = true
	snapshot.transactions[3] = Transaction{ItemId: 100, ItemNum: 9}

	prices := map[int]ShopVo{
		100: {TradeItem: 100, Price: 25},
		200: {TradeItem: 200, Price: 0},
	}

	if got, want := calculateTradeQuote(snapshot, prices), uint32(75); got != want {
		t.Fatalf("calculateTradeQuote() = %d, want %d", got, want)
	}
}

func TestTradeQuoteRefreshCoalescesAndInvalidatesPendingWork(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.UID = 17000001
	r.tradeQuoteLoading = true // Model an already running worker without starting a goroutine.
	r.TransactionArr[0] = &Transaction{ItemId: 100, ItemNum: 1}

	r.queueTradeQuoteRefreshUnsafe()
	firstVersion := r.tradeQuoteVersion
	r.TransactionArr[0].ItemNum = 4
	r.queueTradeQuoteRefreshUnsafe()

	if !r.tradeQuotePending {
		t.Fatal("latest quote refresh was not kept pending")
	}
	if r.tradeQuoteVersion <= firstVersion {
		t.Fatal("quote version did not advance for the newer transaction")
	}

	snapshot, ok := r.tradeQuoteSnapshotUnsafe()
	if !ok {
		t.Fatal("latest quote snapshot was not available")
	}
	if snapshot.version != r.tradeQuoteVersion || snapshot.transactions[0].ItemNum != 4 {
		t.Fatalf("snapshot did not contain latest transaction: %+v", snapshot.transactions[0])
	}
	if r.tradeQuotePending {
		t.Fatal("snapshot did not consume pending refresh")
	}

	r.queueTradeQuoteRefreshUnsafe()
	r.invalidateTradeQuoteUnsafe()
	if r.tradeQuotePending {
		t.Fatal("invalidation left obsolete refresh pending")
	}
	if _, ok := r.tradeQuoteSnapshotUnsafe(); ok {
		t.Fatal("invalidated quote produced a new snapshot")
	}
}

func TestStoreDisplayQueryDoesNotHoldRobotLock(t *testing.T) {
	drv := &blockingStoreDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	driverName := fmt.Sprintf("blocking-store-%d", blockingStoreDriverID.Add(1))
	sql.Register(driverName, drv)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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
