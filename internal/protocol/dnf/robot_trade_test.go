package dnf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

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

func TestTradeQuoteQueryErrorStillClearsQuotedMoney(t *testing.T) {
	release := make(chan struct{})
	close(release)
	drv := &blockingStoreDriver{
		started:  make(chan struct{}, 1),
		release:  release,
		queryErr: errors.New("database unavailable"),
	}
	db := openStoreTestDB(t, drv)
	robotConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		_ = robotConn.Close()
		_ = peerConn.Close()
	})
	cipher := newPartyTestCipher(t)
	r := NewRobotVo(db)
	r.State = StateRun
	r.UID = 17000001
	r.Conn = robotConn
	r.Cipher = cipher
	r.TradeMoney = 123
	r.LastTradeState = true
	r.LastTradeID = 7
	r.TransactionArr[0] = &Transaction{ItemId: 100, ItemNum: 1}
	var previousBody [32]byte
	binary.LittleEndian.PutUint32(previousBody[7:11], 123)
	previousBody[0] = 4
	expectedPrevious, err := buildSendPacket(19, 0, previousBody[:], cipher)
	if err != nil {
		t.Fatalf("build expected previous quote: %v", err)
	}
	var nextBody [32]byte
	nextBody[11] = 4
	expectedNext, err := buildSendPacket(19, 1, nextBody[:], cipher)
	if err != nil {
		t.Fatalf("build expected cleared quote: %v", err)
	}

	r.mu.Lock()
	r.queueTradeQuoteRefreshUnsafe()
	r.mu.Unlock()

	const packetSize = 13 + 32
	raw := make([]byte, packetSize*2)
	_ = peerConn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(peerConn, raw); err != nil {
		t.Fatalf("read trade quote packets: %v", err)
	}
	if !bytes.Equal(raw[:packetSize], expectedPrevious) {
		t.Fatalf("previous quote packet = %x, want %x", raw[:packetSize], expectedPrevious)
	}
	if !bytes.Equal(raw[packetSize:], expectedNext) {
		t.Fatalf("cleared quote packet = %x, want %x", raw[packetSize:], expectedNext)
	}
}
