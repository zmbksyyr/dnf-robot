package store

import (
	"bytes"
	"encoding/binary"
	"testing"

	"robot/internal/shared"
)

func TestDungeonSuppliesCreationAndRelogin(t *testing.T) {
	raw := make([]byte, 249*61)
	if changed, err := EnsureDungeonSupplies(raw); err != nil || !changed {
		t.Fatalf("initial supplies: changed=%t err=%v", changed, err)
	}
	slot := raw[105*61 : 106*61]
	if binary.BigEndian.Uint16(slot[:2]) != 3 || binary.LittleEndian.Uint32(slot[2:6]) != 3037 || binary.LittleEndian.Uint32(slot[7:11]) != 1000 {
		t.Fatalf("initial material record: %x", slot)
	}
	// A later session consumes cubes and obtains another material. Refill
	// only the deficit, retaining the other material and existing slot data.
	binary.LittleEndian.PutUint32(slot[7:11], 617)
	slot[60] = 0x42
	other := raw[106*61 : 107*61]
	WriteInventoryStack(other, shared.EquipmentCatalogItem{ID: 3031}, 17, 3)
	wantOther := bytes.Clone(other)
	if changed, err := EnsureDungeonSupplies(raw); err != nil || !changed {
		t.Fatalf("relogin refill: changed=%t err=%v", changed, err)
	}
	if binary.LittleEndian.Uint32(slot[7:11]) != 1000 || slot[60] != 0x42 || !bytes.Equal(other, wantOther) {
		t.Fatal("refill discarded existing inventory")
	}
	before := bytes.Clone(raw)
	if changed, err := EnsureDungeonSupplies(raw); err != nil || changed || !bytes.Equal(raw, before) {
		t.Fatal("an already supplied character changed on repeated login")
	}
}

func TestDungeonSuppliesCountsAllStacksAndPreservesExcess(t *testing.T) {
	raw := make([]byte, 249*61)
	for index, count := range []int{300, 450} {
		WriteInventoryStack(raw[(105+index)*61:(106+index)*61], shared.EquipmentCatalogItem{ID: 3037}, count, 3)
	}
	if changed, err := EnsureDungeonSupplies(raw); err != nil || !changed {
		t.Fatalf("split stacks: changed=%t err=%v", changed, err)
	}
	if binary.LittleEndian.Uint32(raw[105*61+7:105*61+11])+binary.LittleEndian.Uint32(raw[106*61+7:106*61+11]) != 1000 {
		t.Fatal("split stacks were not topped up to a total of 1000")
	}
	binary.LittleEndian.PutUint32(raw[105*61+7:105*61+11], 2000)
	before := bytes.Clone(raw)
	if changed, err := EnsureDungeonSupplies(raw); err != nil || changed || !bytes.Equal(raw, before) {
		t.Fatal("refill reduced surplus cubes")
	}
}
