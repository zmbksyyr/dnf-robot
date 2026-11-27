package dnf

import (
	"encoding/binary"
	"maps"
	"strconv"
	"testing"
)

func inventoryTestRecord(slot int16, item, value int32, options bool) []byte {
	record := make([]byte, 21)
	binary.LittleEndian.PutUint16(record, uint16(slot))
	binary.LittleEndian.PutUint32(record[2:], uint32(item))
	binary.LittleEndian.PutUint32(record[6:], uint32(value))
	if options {
		// Two Chronicle options, three random options, fortune and fourth option.
		record = append(record, 2)
		record = append(record, make([]byte, 16)...)
		record = append(record, 3)
		record = append(record, make([]byte, 9)...)
		record = append(record, 17, 2, 1, 2, 3, 4)
	} else {
		record = append(record, 0, 0)
	}
	return append(record, 0, 0)
}

func inventoryTestPacket(typ uint16, space byte, records ...[]byte) robotInboundPacket {
	header := 3
	if typ == 13 {
		header = 5
	}
	body := make([]byte, header)
	body[0] = space
	binary.LittleEndian.PutUint16(body[header-2:], uint16(len(records)))
	for _, record := range records {
		body = append(body, record...)
	}
	raw := make([]byte, 15)
	copy(raw[1:3], []byte{byte(typ), byte(typ >> 8)})
	raw = append(raw, body...)
	return robotInboundPacket{typ: typ, data: raw, size: len(raw), isAnti: true}
}

func TestInventoryVariableRecordsPreserveFollowingStack(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	packet := inventoryTestPacket(13, 0,
		inventoryTestRecord(7, 10016, 0, true), inventoryTestRecord(105, 3037, 200, false))
	r.handleStoreTradePacketUnsafe(packet)
	if got := r.InfanMap[105]; got.ItemId != 3037 || got.ItemNum != 200 || len(r.InfanMap) != 2 {
		t.Fatalf("variable record misaligned following material stack: %+v", r.InfanMap)
	}
}

func TestInventoryOtherSpacesDoNotReplaceMainBag(t *testing.T) {
	for _, space := range []byte{1, 2, 3, 7, 12, 28} {
		t.Run(strconv.Itoa(int(space)), func(t *testing.T) {
			r := NewRobotVo(nil)
			r.State = StateRun
			r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3037, ItemNum: 200}
			r.handleStoreTradePacketUnsafe(inventoryTestPacket(13, space, inventoryTestRecord(7, 10016, 1, false)))
			if got := r.InfanMap[105]; len(r.InfanMap) != 1 || got.ItemId != 3037 || got.ItemNum != 200 {
				t.Fatalf("other inventory space replaced main bag: %+v", r.InfanMap)
			}
		})
	}
}

func TestInventoryIncrementalNotificationUpdatesActualStoreMap(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3037, ItemNum: 200}
	version := r.storeInventoryVersion
	packet := inventoryTestPacket(14, 0, inventoryTestRecord(105, 3037, 197, false))
	// Exercise the real packet switch with a plain notification. The cipher is
	// deliberately uninitialized so only the structurally valid plain candidate exists.
	r.parsePacket(packet.data)
	if got := r.InfanMap[105]; got.ItemNum != 197 || got.ItemId != 3037 || r.storeInventoryVersion == version {
		t.Fatalf("type14 left stale store/material inventory: %+v version=%d", r.InfanMap, r.storeInventoryVersion)
	}
	r.parsePacket(inventoryTestPacket(14, 0, inventoryTestRecord(105, -1, 0, false)).data)
	if _, exists := r.InfanMap[105]; exists {
		t.Fatal("type14 empty-slot notification retained consumed stack")
	}
}

func TestInventoryReturnWaitIgnoresLateSnapshots(t *testing.T) {
	r := NewRobotVo(nil)
	r.State = StateRun
	r.ReturnSelectPending = true
	r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3037, ItemNum: 200}
	r.handleStoreTradePacketUnsafe(inventoryTestPacket(13, 0, inventoryTestRecord(105, 3037, 10, false)))
	if r.InfanMap[105].ItemNum != 200 {
		t.Fatal("pending return accepted late inventory snapshot")
	}
}

func TestInventoryMalformedUpdateIsAtomic(t *testing.T) {
	first := inventoryTestRecord(105, 3037, 197, false)
	second := inventoryTestRecord(106, 3037, 12, true)
	valid := inventoryTestPacket(14, 0, first, second)
	for size := 15; size < len(valid.data); size++ {
		r := NewRobotVo(nil)
		r.State = StateRun
		r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3037, ItemNum: 200}
		before := maps.Clone(r.InfanMap)
		packet := valid
		packet.data = packet.data[:size]
		r.handleStoreTradePacketUnsafe(packet)
		if !maps.Equal(before, r.InfanMap) || r.storeInventoryVersion != 0 {
			t.Fatalf("truncated second record partly applied: size=%d inventory=%+v", size, r.InfanMap)
		}
	}
	for _, records := range [][][]byte{
		{first, first},
		{inventoryTestRecord(-1, 3037, 1, false)},
		{inventoryTestRecord(312, 3037, 1, false)},
		{inventoryTestRecord(105, -2, 1, false)},
	} {
		if _, ok := parseMainInventory(inventoryTestPacket(14, 0, records...).data[15:], 14); ok {
			t.Fatalf("accepted invalid inventory records: %x", records)
		}
	}
	for offset, value := range map[int]byte{21: 3, 22: 4} {
		record := append([]byte(nil), first...)
		record[offset] = value
		if _, _, ok := parseMainInventoryRecord(record); ok {
			t.Fatalf("accepted invalid option count at %d", offset)
		}
	}
}

func TestInventoryEnvelopeAndReservedCurrency(t *testing.T) {
	for _, typ := range []uint16{13, 14} {
		packet := inventoryTestPacket(typ, 0, inventoryTestRecord(0, 0, 23, false),
			inventoryTestRecord(105, 3037, 200, false), inventoryTestRecord(106, 3037, 30, false))
		body := packet.data[15:]
		block := 4
		if typ == 13 {
			block = 16
		}
		padded := append([]byte(nil), body...)
		for len(padded) < alignTo(len(body)+4, block) {
			padded = append(padded, 0xa7)
		}
		withHash := append(append([]byte(nil), body...), 1, 2, 3, 4)
		for _, input := range [][]byte{body, withHash, padded} {
			items, ok := parseMainInventory(input, typ)
			if !ok || len(items) != 3 || items[0].ItemNum != 23 || items[106].ItemNum != 30 {
				t.Fatalf("valid main inventory rejected: type=%d bytes=%d items=%+v", typ, len(input), items)
			}
		}
		if _, ok := parseMainInventory(append(padded, 0), typ); ok {
			t.Fatal("accepted unexpected inventory suffix")
		}
	}
	r := NewRobotVo(nil)
	r.State = StateRun
	r.InfanMap[7] = Transaction{ItemPos: 7, ItemId: 10016, ItemNum: 0}
	r.handleStoreTradePacketUnsafe(inventoryTestPacket(14, 0, inventoryTestRecord(0, 0, 0, false)))
	if len(r.InfanMap) != 2 || r.InfanMap[0].ItemId != 0 || r.InfanMap[7].ItemId != 10016 {
		t.Fatalf("currency update changed equipment: %+v", r.InfanMap)
	}
}

func TestInventoryClearsAtCharacterBoundaries(t *testing.T) {
	for name, clearState := range map[string]func(*RobotVo){
		"load":   func(r *RobotVo) { r.Load(UserLoginInfo{}) },
		"return": func(r *RobotVo) { r.finishReturnToSelectUnsafe() },
		"close":  func(r *RobotVo) { r.CloseOut() },
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRobotVo(nil)
			r.State = StateRun
			r.IsWaitingItemList = true
			r.InfanMap[105] = Transaction{ItemPos: 105, ItemId: 3037, ItemNum: 200}
			clearState(r)
			if len(r.InfanMap) != 0 || r.IsWaitingItemList || r.storeInventoryVersion == 0 {
				t.Fatalf("old character inventory survived %s", name)
			}
		})
	}
}
