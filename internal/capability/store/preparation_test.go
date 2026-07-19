package store

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
	"testing"
)

func TestSelectStoreItemsUsesAllowDenyAndMaterialRules(t *testing.T) {
	preparer := Preparer{Env: testPreparationEnv{catalog: []shared.EquipmentCatalogItem{
		{ID: 3037, Level: 1, Slot: "material", Trade: true, BasicMaterial: true, Icon: "stackable/material.img", FieldImage: "material/ore", StackLimit: 1000},
		{ID: 3031, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/cloth", StackLimit: 1000},
		{ID: 3032, Level: 99, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/high", StackLimit: 1000},
		{ID: 7312, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/deny", StackLimit: 1000},
		{ID: 3034, Level: 1, Slot: "material", Trade: true, Icon: "stackable/etc.img", FieldImage: "material/bad_icon", StackLimit: 1000},
		{ID: 3035, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", StackLimit: 1000},
	}}}

	items := preparer.SelectItems(robotcap.Info{Level: 10}, robotconfig.RuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 7,
		StoreItemAllowIDs:      []int{3037, 3031, 3032, 3034, 3035, 7312},
		StoreItemDenyIDs:       []int{7312},
	})

	got := storeItemIDSet(items)
	if len(got) != 1 || !got[3037] {
		t.Fatalf("selected IDs got %v want only basic allowed material 3037", got)
	}
}

func TestSelectStoreItemsFallbacksToAllowIDs(t *testing.T) {
	preparer := Preparer{Env: testPreparationEnv{catalog: []shared.EquipmentCatalogItem{
		{ID: 9001, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/not_allowed", StackLimit: 1000},
	}}}

	items := preparer.SelectItems(robotcap.Info{Level: 10}, robotconfig.RuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 7,
		StoreItemAllowIDs:      []int{3037, 3031},
		StoreItemDenyIDs:       []int{3031},
	})

	if len(items) != 1 || items[0].ID != 3037 || items[0].Slot != "material" {
		t.Fatalf("fallback items got %+v want synthetic material 3037", items)
	}
}

func TestSelectStoreItemsBoundsLargeCatalogSample(t *testing.T) {
	catalog := make([]shared.EquipmentCatalogItem, 5000)
	for i := range catalog {
		catalog[i] = shared.EquipmentCatalogItem{
			ID:         i + 1,
			Level:      1,
			Slot:       "material",
			Trade:      true,
			Icon:       "stackable/material.img",
			FieldImage: "material/item",
			StackLimit: 1000,
		}
	}
	preparer := Preparer{Env: testPreparationEnv{catalog: catalog}}

	items := preparer.SelectItems(robotcap.Info{UID: 17000001, Level: 10}, robotconfig.RuntimeConfig{
		StoreItemSlots:         24,
		StoreInventoryStartBox: 7,
	})

	if len(items) != 24 {
		t.Fatalf("selected items got %d want 24", len(items))
	}
	if got := len(storeItemIDSet(items)); got != len(items) {
		t.Fatalf("selected items contain duplicates: unique=%d items=%d", got, len(items))
	}
}

type testPreparationEnv struct {
	catalog []shared.EquipmentCatalogItem
}

func (e testPreparationEnv) EnsureStorePermissionRecord(uid, cid int) (PermissionStatus, error) {
	return PermissionStatus{}, nil
}

func (e testPreparationEnv) LoadInventory(cid int) ([]byte, error) {
	return nil, nil
}

func (e testPreparationEnv) Logf(format string, args ...interface{}) {}

func (e testPreparationEnv) RandBetween(min, max int) int {
	return min
}

func (e testPreparationEnv) ReplaceStoreStall(uid int, title string, items []StallItem) (StallResult, error) {
	return StallResult{}, nil
}

func (e testPreparationEnv) RepairRobotExpBounds(uid, cid int) (ExpRepairResult, error) {
	return ExpRepairResult{}, nil
}

func (e testPreparationEnv) RobotCID(uid int) (int, error) {
	return 0, nil
}

func (e testPreparationEnv) SaveInventory(cid int, capacity int, raw []byte) error {
	return nil
}

func (e testPreparationEnv) SaveInventoryRaw(cid int, raw []byte) error {
	return nil
}

func (e testPreparationEnv) StackableCatalog() []shared.EquipmentCatalogItem {
	return e.catalog
}

func storeItemIDSet(items []shared.EquipmentCatalogItem) map[int]bool {
	out := make(map[int]bool, len(items))
	for _, item := range items {
		out[item.ID] = true
	}
	return out
}
