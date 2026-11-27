package dnf

import (
	"encoding/binary"

	"robot/internal/shared"
)

// InitOnAccept sends the 334 cipher-key bytes, channel length/string, date
// seed, then the inventory/skill checksum seed. It belongs to the connection,
// so returning to character selection must preserve it.
func parseInventoryChecksumSeed(body []byte) (uint32, bool) {
	const channelOffset = 334
	if len(body) < channelOffset+12 {
		return 0, false
	}
	size := uint64(binary.LittleEndian.Uint32(body[channelOffset:]))
	if size > uint64(len(body)-channelOffset-12) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(body[channelOffset+8+int(size):]), true
}

// Both original binaries wrap the product in signed 32 bits BEFORE dividing
// by ten, then fold its signed high word into the unsigned low word. Promoting
// the product to int64 changes checksums for ordinary large item IDs.
func inventoryItemChecksum(slot int, id, quantity, upgrade, endurance int32) uint32 {
	value := (id + quantity + upgrade + endurance) * int32(slot) * 10
	value /= 10
	return uint32(value&0xffff) + uint32(value>>16)
}

func (r *RobotVo) inventoryChecksumUnsafe() (uint32, bool) {
	if !r.inventoryChecksumSeedKnown || !r.inventoryChecksumKnown || r.inventoryNeedsSnapshot ||
		r.ReturnSelectPending || r.State != StateRun || !r.characterBaseInfo.equipment.known ||
		len(r.InfanMap) != len(r.inventoryChecksumRecords) {
		return 0, false
	}
	var sum uint32
	for slot, record := range r.inventoryChecksumRecords {
		item, size, ok := parseMainInventoryRecord([]byte(record))
		if !ok || size != len(record) || int(item.ItemPos) != slot || item != r.InfanMap[slot] {
			return 0, false
		}
		if slot < 3 {
			if item.ItemId != int32(slot) {
				return 0, false
			}
			continue
		}
		quantity := item.ItemNum
		switch shared.InventoryItemKindForID(item.ItemId) {
		case shared.InventoryItemEquipment:
			quantity = 1 // The wire value is an instance number, not a count.
		case shared.InventoryItemStackable:
		default:
			return 0, false
		}
		sum += inventoryItemChecksum(slot, item.ItemId, quantity, int32(record[10]&0x1f),
			int32(binary.LittleEndian.Uint16([]byte(record[11:13]))))
	}
	for slot := 10; slot < 22; slot++ {
		record := r.characterBaseInfo.equipment.slots[slot]
		if record == "" {
			continue
		}
		if len(record) < 27 || int(record[0]) != slot {
			return 0, false
		}
		id := int32(binary.LittleEndian.Uint32([]byte(record[1:5])))
		if id <= 0 {
			return 0, false
		}
		sum += inventoryItemChecksum(slot, id, 1, int32(record[9]&0x1f),
			int32(binary.LittleEndian.Uint16([]byte(record[10:12]))))
	}
	for slot := 0; slot < 3; slot++ {
		item, exists := r.InfanMap[slot]
		if !exists || item.ItemId != int32(slot) {
			return 0, false
		}
		sum += uint32(item.ItemNum)
	}
	return sum ^ r.inventoryChecksumSeed, true
}
