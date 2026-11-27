package dnf

import (
	"encoding/binary"
	"maps"
)

// The main item space is 0; avatars, equipment, creatures and cargo have
// different headers and records. Original CInventory::MakeItemList iterates
// main slots 3..311 after its three currency records.
const characterInventorySlots = 312

func parseMainInventory(body []byte, typ uint16) (map[int]Transaction, bool) {
	items, _, ok := parseMainInventoryWithRecords(body, typ)
	return items, ok
}

func parseMainInventoryWithRecords(body []byte, typ uint16) (map[int]Transaction, map[int]string, bool) {
	header, block := 3, 4 // type14: space:u8, count:u16; Shift envelope
	if typ == 13 {
		header, block = 5, 16 // space:u8, capacity:u16, count:u16; Anubis
	} else if typ != 14 {
		return nil, nil, false
	}
	if len(body) < header || body[0] != 0 {
		return nil, nil, false
	}
	count := int(binary.LittleEndian.Uint16(body[header-2:]))
	if count > characterInventorySlots || count > (len(body)-header)/25 {
		return nil, nil, false
	}
	items := make(map[int]Transaction, count)
	records := make(map[int]string, count)
	pos := header
	for i := 0; i < count; i++ {
		item, size, ok := parseMainInventoryRecord(body[pos:])
		if !ok {
			return nil, nil, false
		}
		if _, duplicate := items[int(item.ItemPos)]; duplicate {
			return nil, nil, false
		}
		items[int(item.ItemPos)] = item
		records[int(item.ItemPos)] = string(body[pos : pos+size])
		pos += size
	}
	// Ordinary notifications include a four-byte hash and cipher alignment.
	// Native does not require the alignment bytes to be zero. Plain/anti bodies
	// can omit alignment or both the alignment and hash.
	if len(body) != pos && len(body) != pos+4 && len(body) != alignTo(pos+4, block) {
		return nil, nil, false
	}
	return items, records, true
}

func parseMainInventoryRecord(body []byte) (Transaction, int, bool) {
	if len(body) < 25 {
		return Transaction{}, 0, false
	}
	item := Transaction{
		ItemPos: int16(binary.LittleEndian.Uint16(body)),
		ItemId:  int32(binary.LittleEndian.Uint32(body[2:])),
		ItemNum: int32(binary.LittleEndian.Uint32(body[6:])),
	}
	if item.ItemPos < 0 || int(item.ItemPos) >= characterInventorySlots || item.ItemId < -1 {
		return Transaction{}, 0, false
	}
	// The fixed prefix ends with amplification type/value at byte 21.
	// Chronicle options carry int ID + four bytes, up to two entries.
	pos := 21
	take := func(n int) []byte {
		if n > len(body)-pos {
			return nil
		}
		part := body[pos : pos+n]
		pos += n
		return part
	}
	chronicle := take(1)
	if chronicle[0] > 2 || take(int(chronicle[0])*8) == nil {
		return Transaction{}, 0, false
	}
	random := take(1)
	if random == nil || random[0] > 3 || take(int(random[0])*3) == nil {
		return Transaction{}, 0, false
	}
	if random[0] != 0 {
		extra := take(2) // fortune and fourth-option type (0xff means absent)
		if extra == nil || extra[1] != 0xff && (extra[1] > 3 || take(4) == nil) {
			return Transaction{}, 0, false
		}
	}
	if take(2) == nil { // separate upgrade and trade restriction
		return Transaction{}, 0, false
	}
	return item, pos, true
}

func (r *RobotVo) handleInventoryPacketUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 || r.ReturnSelectPending || r.State != StateRun && r.State != StateLogin {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected map[int]Transaction
	var records map[int]string
	for _, candidate := range candidates {
		items, raw, ok := parseMainInventoryWithRecords(candidate.body, packet.typ)
		if !ok {
			continue
		}
		if selected != nil && (!maps.Equal(selected, items) || !maps.Equal(records, raw)) {
			r.inventoryChecksumKnown = false
			return
		}
		selected = items
		records = raw
	}
	if selected == nil {
		for _, candidate := range candidates {
			// Space 3 describes worn equipment (INVEN_TYPE 0), whose records
			// differ from USERINFO. An undecoded update invalidates that snapshot.
			// Only examine alternatives after rejecting a valid main-space body;
			// encrypted bytes can coincidentally start with another space value.
			if packet.typ == 14 && len(candidate.body) >= 3 && candidate.body[0] == 3 {
				r.characterBaseInfo.equipment.known = false
			}
			if len(candidate.body) != 0 && candidate.body[0] == 0 {
				r.inventoryChecksumKnown = false
			}
		}
		return
	}
	if packet.typ == 13 {
		// Preserve the existing inventory-discovery wait for the empty
		// placeholder emitted before the complete command20 reply by siblings.
		if len(selected) == 0 && r.IsWaitingItemList {
			return
		}
		// Full snapshots contain only occupied slots and reserved currencies.
		for slot, item := range selected {
			if item.ItemId == -1 || item.ItemId == 0 && slot != 0 {
				delete(selected, slot)
				delete(records, slot)
			}
		}
		r.InfanMap = selected
		r.inventoryChecksumRecords = records
		r.inventoryChecksumKnown = true
		r.inventoryNeedsSnapshot = false
		r.IsWaitingItemList = false
	} else {
		if len(selected) == 0 {
			return
		}
		if r.InfanMap == nil {
			r.InfanMap = make(map[int]Transaction, len(selected))
		}
		if r.inventoryChecksumRecords == nil {
			r.inventoryChecksumRecords = make(map[int]string, len(selected))
		}
		for slot, item := range selected {
			if item.ItemId == -1 || item.ItemId == 0 && slot != 0 {
				delete(r.InfanMap, slot)
				delete(r.inventoryChecksumRecords, slot)
			} else {
				r.InfanMap[slot] = item
				r.inventoryChecksumRecords[slot] = records[slot]
			}
		}
	}
	// Invalidates an asynchronous store query that copied older quantities.
	r.storeInventoryVersion++
}

func (r *RobotVo) clearCharacterInventoryUnsafe() {
	r.InfanMap = nil
	r.inventoryChecksumRecords = nil
	r.inventoryChecksumKnown = false
	r.inventoryNeedsSnapshot = false
	r.IsWaitingItemList = false
	r.storeInventoryVersion++
}
