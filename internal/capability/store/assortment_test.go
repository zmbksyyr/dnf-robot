package store

import (
	"bytes"
	"encoding/binary"
	"testing"

	"robot/internal/shared"
)

func TestBuildItemPoolClassifiesTradeablePVFItems(t *testing.T) {
	pool := BuildItemPool([]shared.EquipmentCatalogItem{
		{ID: 100, ItemType: 1, Attach: "sealing", Durability: 18},
		{ID: 101, ItemType: 3, Attach: "trade"},
		{ID: 102, ItemType: 4, Attach: "sealing", NoTrade: true},
		{ID: 103, ItemType: 11, Attach: "sealing"},
		{ID: 104, ItemType: 12, Attach: "sealing"},
	}, []shared.EquipmentCatalogItem{
		{ID: 200, Slot: "material", Icon: "Item/stackable/material.img", Attach: "free", BasicMaterial: true, StackLimit: 1000},
		{ID: 201, Slot: "waste", Attach: "trade"},
		{ID: 202, Slot: "recipe", Attach: "trade", StackLimit: 10},
		{ID: 203, Slot: "waste", Attach: "trade", StackLimit: 1},
		{ID: 204, Slot: "material expert job", Path: "stackable/professional/material/product.stk", Attach: "free"},
		{ID: 205, Slot: "material", Icon: "Item/IconMaterial.img", Attach: "free"},
		{ID: 206, Slot: "material", Icon: "Item/stackable/material.img", Attach: "trade", Trade: true, BasicMaterial: true},
		{ID: 207, Slot: "material expert job", Icon: "Item/stackable/MonsterCard_icon.img", Attach: "free"},
	}, 13)

	if len(pool.Equipment) != 1 || pool.Equipment[0].Item.ID != 100 {
		t.Fatalf("equipment pool = %+v", pool.Equipment)
	}
	if pool.Equipment[0].SlotBytes[1] != 1 || pool.Equipment[0].SlotBytes[6] != 13 {
		t.Fatalf("equipment template type=%d intensify=%d", pool.Equipment[0].SlotBytes[1], pool.Equipment[0].SlotBytes[6])
	}
	if pool.Equipment[0].SlotBytes[0] != 1 {
		t.Fatalf("equipment template seal flag=%d, want 1", pool.Equipment[0].SlotBytes[0])
	}
	if got := int(binary.LittleEndian.Uint16(pool.Equipment[0].SlotBytes[11:13])); got != 18 {
		t.Fatalf("equipment template durability=%d, want 18", got)
	}
	if !bytes.Equal(pool.Equipment[0].SlotBytes[7:11], make([]byte, 4)) {
		t.Fatalf("store equipment grade bytes = %x, want zero", pool.Equipment[0].SlotBytes[7:11])
	}
	if got := int(binary.LittleEndian.Uint32(pool.Equipment[0].SlotBytes[2:6])); got != 100 {
		t.Fatalf("equipment template item id = %d", got)
	}
	if len(pool.Stackable) != 1 {
		t.Fatalf("stackable pool size = %d, want 1", len(pool.Stackable))
	}
	if pool.Stackable[0].Kind != PoolMaterial || pool.Stackable[0].MaxCount != 1000 {
		t.Fatalf("material entry = %+v", pool.Stackable[0])
	}
}

func TestItemPoolDrawsNormalPrivateStoreLayoutWithoutDuplicates(t *testing.T) {
	pool := &ItemPool{}
	for id := 1; id <= 30; id++ {
		pool.Stackable = append(pool.Stackable, PoolEntry{Item: shared.EquipmentCatalogItem{ID: id}})
		pool.Equipment = append(pool.Equipment, PoolEntry{Item: shared.EquipmentCatalogItem{ID: 1000 + id}})
	}
	stackable, equipment := pool.Draw(17000001)
	if len(stackable) != 3 || len(equipment) != 4 {
		t.Fatalf("draw sizes stackable=%d equipment=%d", len(stackable), len(equipment))
	}
	assertUniquePoolEntries(t, stackable)
	assertUniquePoolEntries(t, equipment)
}

func assertUniquePoolEntries(t *testing.T, entries []PoolEntry) {
	t.Helper()
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if _, exists := seen[entry.Item.ID]; exists {
			t.Fatalf("duplicate pool item %d", entry.Item.ID)
		}
		seen[entry.Item.ID] = struct{}{}
	}
}
