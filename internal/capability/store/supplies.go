package store

import (
	"encoding/binary"
	"fmt"

	"robot/internal/shared"
)

const (
	ColorlessCubeItemID = 3037
	ColorlessCubeCount  = 1000
	materialRawStart    = 105
	materialRawEnd      = 153
)

// EnsureDungeonSupplies uses the same 61-byte records as store preparation.
// Top up the total stock without discarding existing materials or excess cubes.
func EnsureDungeonSupplies(raw []byte) (bool, error) {
	if len(raw) < materialRawEnd*61 {
		return false, fmt.Errorf("dungeon supply inventory is too short: %d", len(raw))
	}
	total, existing, empty := uint64(0), -1, -1
	for index := materialRawStart; index < materialRawEnd; index++ {
		slot := raw[index*61 : (index+1)*61]
		id := binary.LittleEndian.Uint32(slot[2:6])
		if id == 0 && empty < 0 {
			empty = index
		}
		if id == ColorlessCubeItemID && binary.BigEndian.Uint16(slot[:2]) == 3 {
			total += uint64(binary.LittleEndian.Uint32(slot[7:11]))
			existing = index
		}
	}
	if total >= ColorlessCubeCount {
		return false, nil
	}
	if existing >= 0 {
		slot := raw[existing*61 : (existing+1)*61]
		count := binary.LittleEndian.Uint32(slot[7:11])
		binary.LittleEndian.PutUint32(slot[7:11], count+uint32(ColorlessCubeCount-total))
		return true, nil
	}
	if empty < 0 {
		return false, fmt.Errorf("no material slot available for colorless cubes")
	}
	WriteInventoryStack(raw[empty*61:(empty+1)*61], shared.EquipmentCatalogItem{ID: ColorlessCubeItemID}, ColorlessCubeCount, 3)
	return true, nil
}
