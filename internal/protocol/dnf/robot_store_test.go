package dnf

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

var blockingStoreDriverID atomic.Uint64

type blockingStoreDriver struct {
	started      chan struct{}
	release      chan struct{}
	columns      []string
	rows         [][]driver.Value
	inventoryRaw []byte
	queryErr     error
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

func (c *blockingStoreConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	select {
	case c.driver.started <- struct{}{}:
	default:
	}
	select {
	case <-c.driver.release:
		if c.driver.queryErr != nil {
			return nil, c.driver.queryErr
		}
		if strings.Contains(query, "UNCOMPRESS(") && c.driver.inventoryRaw != nil {
			return &storeTestRows{columns: []string{"inventory"}, rows: [][]driver.Value{{c.driver.inventoryRaw}}}, nil
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
	drv := &blockingStoreDriver{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		rows: [][]driver.Value{
			{"100", "50", "1"},
		},
	}
	db := openStoreTestDB(t, drv)

	r := newStorePacketTestRobot(t, &captureSessionConn{})
	r.DB = db
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
		if ok {
			t.Fatal("stale inventory query reported a sent display")
		}
	case <-time.After(time.Second):
		t.Fatal("store display query did not finish")
	}
	if r.Snapshot().StoreDisplaySent {
		t.Fatal("stale inventory query published a store display")
	}
}

func TestStoreDisplayDoesNotSendWithoutCMD13Match(t *testing.T) {
	release := make(chan struct{})
	close(release)
	drv := &blockingStoreDriver{
		started: make(chan struct{}, 1),
		release: release,
		rows: [][]driver.Value{
			{"200", "50", "1"},
		},
	}
	db := openStoreTestDB(t, drv)
	conn := &captureSessionConn{}
	r := newStorePacketTestRobot(t, conn)
	r.DB = db
	r.UID = 17000001
	r.StoreCreated = true
	r.InfanMap[105] = Transaction{ItemId: 100, ItemPos: 105, ItemNum: 1}

	if r.GetDbDataAndCompleteDisplay() {
		t.Fatal("store display sent without a matching CMD 13 slot")
	}
	if len(conn.written) != 0 || r.Snapshot().StoreDisplaySent {
		t.Fatalf("CMD 90 output bytes=%d sent=%v, want none", len(conn.written), r.Snapshot().StoreDisplaySent)
	}
}

func TestStoreDisplayUsesPreparedInventoryAfterNoCacheRelogin(t *testing.T) {
	release := make(chan struct{})
	close(release)
	raw := make([]byte, 249*61)
	slot := raw[107*61 : 108*61]
	binary.BigEndian.PutUint16(slot[0:2], 3)
	binary.LittleEndian.PutUint32(slot[2:6], 200)
	binary.LittleEndian.PutUint32(slot[7:11], 7)
	drv := &blockingStoreDriver{
		started:      make(chan struct{}, 1),
		release:      release,
		rows:         [][]driver.Value{{"200", "50", "7"}},
		inventoryRaw: raw,
	}
	db := openStoreTestDB(t, drv)
	r := newStorePacketTestRobot(t, &captureSessionConn{})
	r.DB = db
	r.UID = 17000001
	// Runtime CID is the character-list slot used by CMD 4, not charac_no.
	// Prepared inventory must therefore resolve the database character by UID.
	r.CID = 0
	r.StoreCreated = true

	if !r.GetDbDataAndCompleteDisplay() {
		t.Fatal("prepared inventory did not produce CMD 90")
	}
	if len(r.LastStoreDisplay) != 1 || r.LastStoreDisplay[0].BoxIndex != 107 || r.LastStoreDisplay[0].Count != 7 {
		t.Fatalf("prepared display = %+v", r.LastStoreDisplay)
	}
}

func TestDecodePreparedStoreInventoryUsesGlobalRawPositions(t *testing.T) {
	raw := make([]byte, 249*61)
	equipment := raw[9*61 : 10*61]
	binary.BigEndian.PutUint16(equipment[0:2], 1)
	binary.LittleEndian.PutUint32(equipment[2:6], 12515)
	material := raw[107*61 : 108*61]
	binary.BigEndian.PutUint16(material[0:2], 3)
	binary.LittleEndian.PutUint32(material[2:6], 3243)
	binary.LittleEndian.PutUint32(material[7:11], 1000)

	got := decodePreparedStoreInventory(raw)
	if len(got) != 2 || got[9].ItemId != 12515 || got[9].ItemNum != 0 || got[107].ItemId != 3243 || got[107].ItemNum != 1000 {
		t.Fatalf("decoded inventory = %+v", got)
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
		{name: "item list", send: func(r *RobotVo) bool { return r.GetCompleteDisplay(0) }, displayFailed: true},
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

func TestStoreDisplayCapsDirectInputAtSevenItems(t *testing.T) {
	conn := &captureSessionConn{}
	r := newStorePacketTestRobot(t, conn)
	items := make([]StoreInfo, 8)
	for index := range items {
		items[index] = StoreInfo{Index: index, ItemID: 1000 + index, BoxIndex: 7 + index, Price: 10, Count: 1}
	}
	if !r.CompleteDisplay("store", items) {
		t.Fatal("display send failed")
	}
	if len(r.LastStoreDisplay) != privateStoreDisplayLimit || len(r.storeDisplayCandidates) != privateStoreDisplayLimit {
		t.Fatalf("display=%d candidates=%d want=%d", len(r.LastStoreDisplay), len(r.storeDisplayCandidates), privateStoreDisplayLimit)
	}
	if got := r.Snapshot().StoreDisplayItems; got != privateStoreDisplayLimit {
		t.Fatalf("snapshot display items=%d want=%d", got, privateStoreDisplayLimit)
	}
}

func TestStoreCMD13EmptyWhileWaitingRetainsInventoryUntilFullReply(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.IsWaitingItemList = true
	r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3035, ItemNum: 20}
	version := r.storeInventoryVersion

	r.handleStoreTradePacketUnsafe(storeInventoryPacket(nil))
	if !r.IsWaitingItemList {
		t.Fatal("empty CMD 13 ended the inventory wait")
	}
	if r.storeInventoryVersion != version || len(r.InfanMap) != 1 || r.InfanMap[105].ItemId != 3035 {
		t.Fatalf("empty CMD 13 replaced retained inventory: version=%d inventory=%+v", r.storeInventoryVersion, r.InfanMap)
	}

	want := []Transaction{
		{ItemPos: 105, ItemId: 3037, ItemNum: 200},
		{ItemPos: 7, ItemId: 10016, ItemNum: 0},
	}
	r.handleStoreTradePacketUnsafe(storeInventoryPacket(want))
	if r.IsWaitingItemList {
		t.Fatal("complete CMD 13 did not end the inventory wait")
	}
	if r.storeInventoryVersion != version+1 || len(r.InfanMap) != len(want) {
		t.Fatalf("complete CMD 13 version=%d inventory=%+v", r.storeInventoryVersion, r.InfanMap)
	}
	for _, item := range want {
		if got := r.InfanMap[int(item.ItemPos)]; got.ItemId != item.ItemId || got.ItemNum != item.ItemNum {
			t.Fatalf("inventory slot %d = %+v, want %+v", item.ItemPos, got, item)
		}
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

func TestStoreCreateAckIgnoresLateRejection(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.RobotTyp = 2
	r.handleStoreTradePacketUnsafe(storeReplyPacket(88, 1, 0))
	if !r.StoreCreated || r.StoreCreateRejected || r.LastStoreError != 0 {
		t.Fatalf("create ack state created=%v rejected=%v err=%#x", r.StoreCreated, r.StoreCreateRejected, r.LastStoreError)
	}

	r.handleStoreTradePacketUnsafe(storeReplyPacket(88, 0, 0x3f))
	if !r.StoreCreated || r.StoreCreateRejected || r.LastStoreError != 0 {
		t.Fatalf("late rejection overwrote create state created=%v rejected=%v err=%#x", r.StoreCreated, r.StoreCreateRejected, r.LastStoreError)
	}
}

func TestDisjointAckIgnoresLatePositionError(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.RobotTyp = 3
	r.handleStoreTradePacketUnsafe(storeReplyPacket(238, 1, 0))
	if !r.DisjointDirectAck || !r.DisjointActive || r.LastDisjointError != 0 {
		t.Fatalf("disjoint ack state direct=%v active=%v err=%#x", r.DisjointDirectAck, r.DisjointActive, r.LastDisjointError)
	}

	r.handleStoreTradePacketUnsafe(storeReplyPacket(238, 0, 0xbe))
	if !r.DisjointDirectAck || !r.DisjointActive || r.LastDisjointError != 0 {
		t.Fatalf("late disjoint error overwrote active state direct=%v active=%v err=%#x", r.DisjointDirectAck, r.DisjointActive, r.LastDisjointError)
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

func TestStoreDisplayError11RetriesPrefixesThenIndividualSlots(t *testing.T) {
	conn := &captureSessionConn{}
	r := newStorePacketTestRobot(t, conn)
	r.PacketID = 20
	r.RobotTyp = 2
	r.PendingStoreTitle = "store"
	items := []StoreInfo{
		{Index: 0, ItemID: 100, BoxIndex: 107, Price: 10, Count: 1},
		{Index: 1, ItemID: 200, BoxIndex: 108, Price: 20, Count: 1},
		{Index: 2, ItemID: 300, BoxIndex: 9, Price: 30, Count: 1},
	}
	if !r.CompleteDisplay("store", items) {
		t.Fatal("initial display send failed")
	}

	wants := [][]int{{100, 200}, {100}, {200}, {300}}
	for attempt, want := range wants {
		r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 0, 0x11))
		if r.StoreDisplayRejected {
			t.Fatalf("attempt %d rejected before retries were exhausted", attempt+1)
		}
		if len(r.LastStoreDisplay) != len(want) {
			t.Fatalf("attempt %d items = %+v, want IDs %v", attempt+1, r.LastStoreDisplay, want)
		}
		for index, itemID := range want {
			if r.LastStoreDisplay[index].ItemID != itemID || r.LastStoreDisplay[index].Index != index {
				t.Fatalf("attempt %d item %d = %+v, want ID %d compact index %d", attempt+1, index, r.LastStoreDisplay[index], itemID, index)
			}
		}
	}

	r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 0, 0x11))
	if !r.StoreDisplayRejected || r.LastStoreError != 0x11 {
		t.Fatalf("exhausted retries rejected=%v error=%#x", r.StoreDisplayRejected, r.LastStoreError)
	}
	if r.PacketID != 25 {
		t.Fatalf("packet id = %d, want five display attempts", r.PacketID)
	}
}

func TestSevenItemStoreKeepsGradualDowngradeFallback(t *testing.T) {
	conn := &captureSessionConn{}
	r := newStorePacketTestRobot(t, conn)
	r.RobotTyp = 2
	r.PendingStoreTitle = "store"
	items := make([]StoreInfo, privateStoreDisplayLimit)
	for index := range items {
		items[index] = StoreInfo{Index: index, ItemID: 1000 + index, BoxIndex: 105 + index, Price: 10, Count: 1000}
	}
	if !r.CompleteDisplay("store", items) {
		t.Fatal("initial seven-item display send failed")
	}
	r.handleStoreTradePacketUnsafe(storeReplyPacket(90, 0, 0x11))
	if r.StoreDisplayRejected {
		t.Fatal("seven-item display rejected without trying a smaller valid set")
	}
	if len(r.LastStoreDisplay) != privateStoreDisplayLimit-1 {
		t.Fatalf("first downgrade has %d items, want %d", len(r.LastStoreDisplay), privateStoreDisplayLimit-1)
	}
}

func TestReconcileStoreDisplayUsesOnlineSlotsAndAvailableCounts(t *testing.T) {
	rows := [][]string{
		{"3035", "100", "7"},
		{"3034", "200", "5"},
		{"3035", "300", "2"},
	}
	inventory := map[int]Transaction{
		105: {ItemPos: 105, ItemId: 3035, ItemNum: 3},
		106: {ItemPos: 106, ItemId: 3035, ItemNum: 9},
	}

	got := reconcileStoreDisplay(rows, inventory)
	if len(got) != 2 {
		t.Fatalf("store items = %d, want 2: %+v", len(got), got)
	}
	if got[0].BoxIndex != 106 || got[0].Count != 7 {
		t.Fatalf("first item = %+v, want slot 106 count 7", got[0])
	}
	if got[1].BoxIndex != 105 || got[1].Count != 2 {
		t.Fatalf("second item = %+v, want slot 105 count 2", got[1])
	}
	if got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("store indexes are not compact: %+v", got)
	}
	if got[0].BoxType != 0 || got[1].BoxType != 0 {
		t.Fatalf("material item spaces = %d/%d, want 0/0", got[0].BoxType, got[1].BoxType)
	}
}

func TestReconcileStoreDisplayCapsConfiguredStackCountAtLiveQuantity(t *testing.T) {
	rows := [][]string{{"3035", "100", "7"}}
	inventory := map[int]Transaction{105: {ItemPos: 105, ItemId: 3035, ItemNum: 3}}

	got := reconcileStoreDisplay(rows, inventory)
	if len(got) != 1 || got[0].Count != 3 || got[0].BoxIndex != 105 {
		t.Fatalf("reconciled store = %+v, want slot 105 live count 3", got)
	}
}

func TestReconcileStoreDisplayPreservesSevenKindsWhenLiveStacksAreSmaller(t *testing.T) {
	rows := [][]string{
		{"3101", "100", "1000"},
		{"3102", "100", "1000"},
		{"3103", "100", "1000"},
		{"10001", "100", "1"},
		{"10002", "100", "1"},
		{"10003", "100", "1"},
		{"10004", "100", "1"},
	}
	inventory := map[int]Transaction{
		105: {ItemPos: 105, ItemId: 3101, ItemNum: 3},
		106: {ItemPos: 106, ItemId: 3102, ItemNum: 1},
		107: {ItemPos: 107, ItemId: 3103, ItemNum: 2},
		9:   {ItemPos: 9, ItemId: 10001, ItemNum: 0},
		10:  {ItemPos: 10, ItemId: 10002, ItemNum: 1},
		11:  {ItemPos: 11, ItemId: 10003, ItemNum: 1},
		12:  {ItemPos: 12, ItemId: 10004, ItemNum: 1},
	}

	got := reconcileStoreDisplay(rows, inventory)
	if len(got) != privateStoreDisplayLimit {
		t.Fatalf("store items = %d, want seven kinds: %+v", len(got), got)
	}
	for index, want := range []int{3, 1, 2, 1, 1, 1, 1} {
		if got[index].Count != want {
			t.Fatalf("item %d count = %d, want %d: %+v", index, got[index].Count, want, got)
		}
	}
}

func TestReconcileStoreDisplayAcceptsNonStackableOnlineItem(t *testing.T) {
	rows := [][]string{{"10016", "100000", "1"}}
	inventory := map[int]Transaction{7: {ItemPos: 7, ItemId: 10016, ItemNum: 0}}

	got := reconcileStoreDisplay(rows, inventory)
	if len(got) != 1 || got[0].Count != 1 || got[0].BoxType != 0 || got[0].BoxIndex != 7 {
		t.Fatalf("reconciled equipment = %+v, want type 0 slot 7 count 1", got)
	}
}

func TestReconcileStoreDisplayCapsNormalStoreAtSevenItems(t *testing.T) {
	rows := make([][]string, 0, 20)
	inventory := make(map[int]Transaction, 20)
	for index := 0; index < 20; index++ {
		itemID := 10000 + index
		rows = append(rows, []string{strconv.Itoa(itemID), "100000", "1"})
		inventory[index+7] = Transaction{ItemPos: int16(index + 7), ItemId: int32(itemID), ItemNum: 1}
	}
	if got := reconcileStoreDisplay(rows, inventory); len(got) != 7 {
		t.Fatalf("store items = %d, want normal store display limit 7", len(got))
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

func storeInventoryPacket(items []Transaction) robotInboundPacket {
	body := make([]byte, 5+len(items)*25)
	binary.LittleEndian.PutUint16(body[3:5], uint16(len(items)))
	for index, item := range items {
		offset := 5 + index*25
		binary.LittleEndian.PutUint16(body[offset:offset+2], uint16(item.ItemPos))
		binary.LittleEndian.PutUint32(body[offset+2:offset+6], uint32(item.ItemId))
		binary.LittleEndian.PutUint32(body[offset+6:offset+10], uint32(item.ItemNum))
	}
	raw := make([]byte, 15+len(body))
	copy(raw[15:], body)
	return robotInboundPacket{data: raw, size: len(raw), flag: 0, typ: 13, isAnti: true}
}
