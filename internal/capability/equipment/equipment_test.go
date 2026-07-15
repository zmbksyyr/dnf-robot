package equipment

import (
	"math/rand"
	"testing"

	"robot/internal/shared"
)

func TestWriteEquipSlotUsesHighIntensify(t *testing.T) {
	opt := SlotOptions{IntensifyMin: 0, IntensifyMax: 10}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		WriteEquipSlot(raw, shared.EquipmentCatalogItem{ID: 1000 + i, ItemType: 3}, rng, opt)
		if raw[6] < 7 || raw[6] > 10 {
			t.Fatalf("armor intensify got %d want 7..10", raw[6])
		}
	}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		WriteEquipSlot(raw, shared.EquipmentCatalogItem{ID: 2000 + i, ItemType: 1}, rng, opt)
		if raw[6] < 8 || raw[6] > 15 {
			t.Fatalf("weapon intensify got %d want 8..15", raw[6])
		}
	}
}

func TestAvatarSetSelectionRandomizesAcrossSixSlotSets(t *testing.T) {
	candidates := testSetCandidates(9, 6)
	selected := SelectAvatarSetItems(candidates, 2, func(n int) int { return n - 1 })
	if len(selected) != 6 {
		t.Fatalf("selected slots got %d want 6", len(selected))
	}
	for _, item := range selected {
		if item.SetKey != "variety" {
			t.Fatalf("selected set %q want variety", item.SetKey)
		}
	}
}

func TestAvatarSetSelectionFallsBackBelowSixSlots(t *testing.T) {
	candidates := testSetCandidates(5, 4)
	selected := SelectAvatarSetItems(candidates, 2, func(n int) int { return n - 1 })
	if len(selected) != 5 {
		t.Fatalf("selected slots got %d want best five-slot set", len(selected))
	}
	for _, item := range selected {
		if item.SetKey != "quality" {
			t.Fatalf("selected set %q want quality", item.SetKey)
		}
	}
}

func TestEquipmentSetSelectionKeepsHighestScore(t *testing.T) {
	candidates := testSetCandidates(9, 6)
	selected := SelectSetItems(candidates, 2, func(n int) int { return n - 1 })
	if len(selected) != 9 {
		t.Fatalf("selected slots got %d want highest-score nine-slot set", len(selected))
	}
	for _, item := range selected {
		if item.SetKey != "quality" {
			t.Fatalf("selected set %q want quality", item.SetKey)
		}
	}
}

func testSetCandidates(qualitySlots, varietySlots int) map[int][]shared.EquipmentCatalogItem {
	out := make(map[int][]shared.EquipmentCatalogItem)
	for slot := 0; slot < qualitySlots; slot++ {
		out[slot] = append(out[slot], shared.EquipmentCatalogItem{ID: 1000 + slot, SetKey: "quality", Level: 100, Rarity: 5})
	}
	for slot := 0; slot < varietySlots; slot++ {
		out[slot] = append(out[slot], shared.EquipmentCatalogItem{ID: 2000 + slot, SetKey: "variety"})
	}
	return out
}
