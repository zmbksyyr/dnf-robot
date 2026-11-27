package shared

import "sync/atomic"

type InventoryItemKind byte

const (
	InventoryItemUnknown InventoryItemKind = iota
	InventoryItemEquipment
	InventoryItemStackable
)

var inventoryItemKinds atomic.Pointer[map[int32]InventoryItemKind]

// SetInventoryItemKinds publishes identities from both current PVF catalogs.
// A missing or conflicting identity cannot establish whether the inventory's
// value field is a stack quantity or an equipment instance number.
func SetInventoryItemKinds(equipment, stackable []EquipmentCatalogItem) {
	kinds := make(map[int32]InventoryItemKind, len(equipment)+len(stackable))
	for i, entries := range [][]EquipmentCatalogItem{equipment, stackable} {
		kind := InventoryItemEquipment
		if i == 1 {
			kind = InventoryItemStackable
		}
		for _, entry := range entries {
			if entry.ID <= 0 || int64(entry.ID) > 1<<31-1 {
				continue
			}
			id := int32(entry.ID)
			if previous, exists := kinds[id]; exists && previous != kind {
				kinds[id] = InventoryItemUnknown
			} else if !exists {
				kinds[id] = kind
			}
		}
	}
	inventoryItemKinds.Store(&kinds)
}

func InventoryItemKindForID(id int32) InventoryItemKind {
	if kinds := inventoryItemKinds.Load(); kinds != nil {
		return (*kinds)[id]
	}
	return InventoryItemUnknown
}
