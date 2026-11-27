package dnf

import (
	"database/sql/driver"
	"encoding/binary"
	"maps"
	"testing"
	"time"
)

func inventoryDeleteReply(space byte, success bool, slots ...int16) []byte {
	body := []byte{0, 0x11, space}
	if success {
		body = []byte{1, space, byte(len(slots))}
		for _, slot := range slots {
			record := make([]byte, 8)
			binary.LittleEndian.PutUint16(record, uint16(slot))
			binary.LittleEndian.PutUint32(record[2:], 1) // deleted amount, not remaining
			binary.LittleEndian.PutUint16(record[6:], 2)
			body = append(body, record...)
		}
	}
	raw := make([]byte, 15)
	raw[0], raw[1] = 1, 18
	return append(raw, body...)
}

func TestInventoryDeleteReplyRejectsStaleStoreQuery(t *testing.T) {
	drv := &blockingStoreDriver{
		started: make(chan struct{}, 1), release: make(chan struct{}),
		rows: [][]driver.Value{{"4183", "50", "1"}},
	}
	r := NewRobotVo(openStoreTestDB(t, drv))
	r.State, r.StoreCreated = StateRun, true
	r.InfanMap[153] = Transaction{ItemPos: 153, ItemId: 4183, ItemNum: 3}
	result := make(chan bool, 1)
	go func() { result <- r.GetDbDataAndCompleteDisplay() }()
	select {
	case <-drv.started:
	case <-time.After(time.Second):
		t.Fatal("store query did not start")
	}
	r.mu.Lock()
	r.parsePacket(inventoryDeleteReply(0, true, 153))
	// Even an intervening complete snapshot must not revive this old query.
	r.parsePacket(inventoryTestPacket(13, 0, inventoryTestRecord(153, 4183, 2, false)).data)
	r.mu.Unlock()
	close(drv.release)
	select {
	case sent := <-result:
		if sent || r.StoreDisplaySent {
			t.Fatal("query from before deletion published old quantities")
		}
	case <-time.After(time.Second):
		t.Fatal("store query did not finish")
	}
}

func TestInventoryDeleteReplyDiscardsCachedStoreRetries(t *testing.T) {
	r := newStorePacketTestRobot(t, &captureSessionConn{})
	items := []StoreInfo{{ItemID: 4183, Count: 3, BoxIndex: 153}, {ItemID: 3037, Count: 1, BoxIndex: 154}}
	if !r.CompleteDisplay("store", items) || len(r.storeDisplayRetryPlans) == 0 {
		t.Fatal("could not prepare the original store attempt")
	}
	packetID := r.PacketID
	r.parsePacket(inventoryDeleteReply(0, true, 153))
	r.StoreDisplaySent = false
	if r.CompleteDisplay("store", items) || r.PacketID != packetID {
		t.Fatal("explicit display reused quantities from unavailable inventory")
	}
	r.parsePacket(inventoryTestPacket(13, 0, inventoryTestRecord(153, 4183, 2, false)).data)
	if r.retryPrivateStoreDisplayUnsafe() || len(r.storeDisplayCandidates) != 0 {
		t.Fatal("new snapshot revived the pre-deletion retry plan")
	}
}

func TestInventoryDeleteReplyTwofishFailureAndMultipleEntries(t *testing.T) {
	for _, input := range [][]byte{
		inventoryDeleteReply(0, false), inventoryDeleteReply(0, true, 153, 154),
	} {
		r := NewRobotVo(nil)
		r.State, r.Cipher = StateRun, newPartyTestCipher(t)
		body := make([]byte, alignTo(len(input)-15+4, 16))
		copy(body, input[15:])
		raw := makeEncryptedPartyRecvPacket(t, r.Cipher, 18, body)
		raw[0] = 1
		r.parsePacket(raw)
		if !r.inventoryNeedsSnapshot {
			t.Fatalf("Twofish reply did not invalidate inventory: %x", input[15:])
		}
	}
}

func TestInventoryDeleteReplyInvalidatesUncorrelatedState(t *testing.T) {
	for _, success := range []bool{true, false} {
		r := NewRobotVo(nil)
		r.State = StateRun
		r.InfanMap[153] = Transaction{ItemPos: 153, ItemId: 4183, ItemNum: 3}
		r.InfanMap[9] = Transaction{ItemPos: 9, ItemId: 27600, ItemNum: 2141828876}
		version := r.storeInventoryVersion
		r.parsePacket(inventoryDeleteReply(0, success, 153))
		if len(r.InfanMap) != 0 || r.storeInventoryVersion == version || !r.inventoryNeedsSnapshot {
			t.Fatalf("delete reply retained uncorrelated quantities: success=%t inventory=%+v", success, r.InfanMap)
		}
		// Neither an incremental update nor database preparation establishes a
		// complete baseline after a possibly partial deletion.
		r.parsePacket(inventoryTestPacket(14, 0, inventoryTestRecord(153, 4183, 2, false)).data)
		r.DB = openStoreTestDB(t, &blockingStoreDriver{started: make(chan struct{}, 1)})
		r.StoreCreated = true
		if r.GetDbDataAndCompleteDisplay() {
			t.Fatal("unavailable inventory allowed a store display")
		}
		// A complete, independently supplied inventory becomes usable again.
		r.parsePacket(inventoryTestPacket(13, 0, inventoryTestRecord(153, 4183, 2, false)).data)
		if r.InfanMap[153].ItemNum != 2 || r.inventoryNeedsSnapshot {
			t.Fatal("full snapshot did not restore inventory")
		}
	}
}

func TestInventoryDeleteReplyTransportAndSnapshotBoundaries(t *testing.T) {
	for _, transport := range []string{"plain", "encrypted", "anti"} {
		t.Run(transport, func(t *testing.T) {
			conn := &captureSessionConn{}
			r := newStorePacketTestRobot(t, conn)
			r.InfanMap[153] = Transaction{ItemPos: 153, ItemId: 4183, ItemNum: 3}
			raw := inventoryDeleteReply(0, true, 153)
			if transport == "encrypted" {
				body := make([]byte, alignTo(len(raw)-15+4, 16))
				copy(body, raw[15:])
				copy(body[len(raw)-15:], []byte{0xa7, 0xb8, 0xc9, 0xda})
				raw = makeEncryptedPartyRecvPacket(t, r.Cipher, 18, body)
				raw[0] = 1
			}
			if transport == "anti" {
				r.handleStoreTradePacketUnsafe(robotInboundPacket{typ: 18, flag: 1, data: raw, size: len(raw), isAnti: true})
			} else {
				r.parsePacket(raw)
			}
			if !r.inventoryNeedsSnapshot || len(r.InfanMap) != 0 {
				t.Fatal("delete reply left inventory usable")
			}
			if r.PacketID != 0 {
				t.Fatal("delete reply sent an automatic inventory-sort request")
			}
			r.parsePacket(inventoryTestPacket(14, 0, inventoryTestRecord(153, 4183, 2, false)).data)
			if !r.inventoryNeedsSnapshot {
				t.Fatal("incremental update established a complete baseline")
			}
			r.IsWaitingItemList = true
			r.parsePacket(inventoryTestPacket(13, 0).data)
			if !r.inventoryNeedsSnapshot {
				t.Fatal("empty discovery placeholder restored inventory")
			}
			r.parsePacket(inventoryTestPacket(13, 1, inventoryTestRecord(153, 4183, 2, false)).data)
			if !r.inventoryNeedsSnapshot {
				t.Fatal("another item space restored the main inventory")
			}
			r.CloseOut()
			if r.inventoryNeedsSnapshot {
				t.Fatal("inventory invalidation survived the session boundary")
			}
		})
	}
}

func TestInventoryDeleteReplyRejectsIncompleteOrInvalidProgress(t *testing.T) {
	valid := inventoryDeleteReply(0, true, 153, 154)[15:]
	for size := 0; size < len(valid); size++ {
		if _, _, ok := parseInventoryDeleteReply(valid[:size]); ok {
			t.Fatalf("accepted truncated delete progress: bytes=%d", size)
		}
	}
	for _, offset := range []int{5, 13} {
		for _, quantity := range []uint32{0, 0x80000000, 0xffffffff} {
			bad := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint32(bad[offset:], quantity)
			if _, _, ok := parseInventoryDeleteReply(bad); ok {
				t.Fatalf("accepted invalid deletion amount: %x", bad)
			}
		}
	}
	for _, slot := range []uint16{312, 0x7fff, 0xffff} {
		bad := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint16(bad[3:], slot)
		if _, _, ok := parseInventoryDeleteReply(bad); ok {
			t.Fatalf("accepted invalid main slot: %d", slot)
		}
	}
	for _, body := range [][]byte{{2, 0, 0}, {1, 0, 101}, append(valid, 0)} {
		if _, _, ok := parseInventoryDeleteReply(body); ok {
			t.Fatalf("accepted unsupported delete reply: %x", body)
		}
	}
	// Error precedes space, so error 0 does not make another space the main bag.
	space, changed, ok := parseInventoryDeleteReply([]byte{0, 0, 1})
	if !ok || !changed || space != 1 {
		t.Fatal("failure space/error order reversed")
	}
	for _, body := range [][]byte{valid, {0, 0x11, 0}} {
		for _, size := range []int{len(body), len(body) + 4, alignTo(len(body)+4, 16)} {
			padded := make([]byte, size)
			copy(padded, body)
			for i := len(body); i < size; i++ {
				padded[i] = 0xa7
			}
			if _, changed, ok := parseInventoryDeleteReply(padded); !ok || !changed {
				t.Fatalf("rejected supported delete envelope: bytes=%d", size)
			}
		}
	}
}

func TestInventoryDeleteReplyScopeAndNoop(t *testing.T) {
	for _, mode := range []string{"other-space-success", "other-space-failure", "notification", "return", "empty-success", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			r := NewRobotVo(nil)
			r.State = StateRun
			r.InfanMap[153] = Transaction{ItemPos: 153, ItemId: 4183, ItemNum: 3}
			before := maps.Clone(r.InfanMap)
			raw := inventoryDeleteReply(0, true, 153)
			switch mode {
			case "other-space-success":
				raw = inventoryDeleteReply(1, true, 3)
			case "other-space-failure":
				raw = inventoryDeleteReply(1, false)
			case "notification":
				raw[0] = 0 // NOTI18 is item movement, a separate wire format.
			case "return":
				r.ReturnSelectPending = true
			case "empty-success":
				raw = inventoryDeleteReply(0, true)
			case "stopped":
				r.State = StateStop
			}
			r.parsePacket(raw)
			if !maps.Equal(before, r.InfanMap) || r.storeInventoryVersion != 0 {
				t.Fatalf("unrelated/noop reply changed main inventory: %+v", r.InfanMap)
			}
		})
	}
}
