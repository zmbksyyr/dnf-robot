package store

import (
	"encoding/binary"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
	"testing"
)

func TestPreparePoolInventoryUsesSeparateBagStarts(t *testing.T) {
	pool := &ItemPool{}
	for id := 1; id <= 12; id++ {
		kind := PoolMaterial
		if id%2 == 0 {
			kind = PoolConsumable
		}
		pool.Stackable = append(pool.Stackable, PoolEntry{
			Item: shared.EquipmentCatalogItem{ID: 3000 + id}, Kind: kind, MaxCount: 100,
		})
		entry := PoolEntry{Item: shared.EquipmentCatalogItem{ID: 10000 + id}, Kind: PoolEquipment, MaxCount: 1}
		entry.SlotBytes[1] = 1
		binary.LittleEndian.PutUint32(entry.SlotBytes[2:6], uint32(entry.Item.ID))
		entry.SlotBytes[6] = 13
		pool.Equipment = append(pool.Equipment, entry)
	}
	var saved []byte
	var stalls []StallItem
	env := testPreparationEnv{saved: &saved, stalls: &stalls}
	preparer := Preparer{Env: env, Pool: pool, WorldHorns: NewWorldHornCache()}
	rc := robotconfig.Default()
	rc.StoreEquipmentStartBox = 7
	rc.StoreConsumableStartBox = 56
	rc.StoreMaterialStartBox = 105
	if err := preparer.EnsureInventoryAndStall(robotcap.Info{UID: 17000001, CID: 1}, rc); err != nil {
		t.Fatal(err)
	}
	if len(stalls) != 24 {
		t.Fatalf("stall rows = %d, want 24", len(stalls))
	}
	if len(saved) != 249*61 {
		t.Fatalf("saved inventory bytes = %d", len(saved))
	}
	assertInventoryRangeType(t, saved, 7, 12, 1)
	assertInventoryRangeType(t, saved, 56, 6, 2)
	assertInventoryRangeType(t, saved, 105, 6, 3)
}

func assertInventoryRangeType(t *testing.T, raw []byte, startBox, count, inventoryType int) {
	t.Helper()
	for index := 0; index < count; index++ {
		rawIndex := startBox + index + 2
		slot := raw[rawIndex*61 : (rawIndex+1)*61]
		if int(binary.BigEndian.Uint16(slot[:2])) != inventoryType {
			t.Fatalf("box=%d inventory type=%d want=%d", startBox+index, binary.BigEndian.Uint16(slot[:2]), inventoryType)
		}
		if binary.LittleEndian.Uint32(slot[2:6]) == 0 {
			t.Fatalf("box=%d is empty", startBox+index)
		}
	}
}

func TestStorePoolPriceKeepsStackTotalInSignedProtocolRange(t *testing.T) {
	env := testPreparationEnv{randValue: 5000000}
	price := storePoolPrice(env, robotconfig.RuntimeConfig{StorePriceMin: 100000, StorePriceMax: 5000000}, 2000)
	if int64(price)*2000 > int64(^uint32(0)>>1) {
		t.Fatalf("price=%d count=2000 exceeds signed protocol total", price)
	}
}

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
	catalog   []shared.EquipmentCatalogItem
	saved     *[]byte
	stalls    *[]StallItem
	randValue int
}

func (e testPreparationEnv) EnsureStorePermissionRecord(uid, cid int) (PermissionStatus, error) {
	return PermissionStatus{}, nil
}

func (e testPreparationEnv) LoadInventory(cid int) ([]byte, error) {
	return nil, nil
}

func (e testPreparationEnv) Logf(format string, args ...interface{}) {}

func (e testPreparationEnv) RandBetween(min, max int) int {
	if e.randValue > 0 {
		return e.randValue
	}
	return min
}

func (e testPreparationEnv) ReplaceStoreStall(uid int, title string, items []StallItem) (StallResult, error) {
	if e.stalls != nil {
		*e.stalls = append([]StallItem(nil), items...)
	}
	return StallResult{StallRows: len(items), ConfigRows: 1}, nil
}

func (e testPreparationEnv) RobotCID(uid int) (int, error) {
	return 0, nil
}

func (e testPreparationEnv) SaveInventory(cid int, capacity int, raw []byte) error {
	if e.saved != nil {
		*e.saved = append([]byte(nil), raw...)
	}
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
