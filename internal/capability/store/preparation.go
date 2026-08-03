package store

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
	"strings"
)

type Preparer struct {
	Env        PreparationEnv
	WorldHorns *WorldHornCache
	Pool       *ItemPool
}

type PreparationEnv interface {
	EnsureStorePermissionRecord(uid, cid int) (PermissionStatus, error)
	LoadInventory(cid int) ([]byte, error)
	Logf(format string, args ...interface{})
	RandBetween(min, max int) int
	ReplaceStoreStall(uid int, title string, items []StallItem) (StallResult, error)
	SaveInventory(cid int, capacity int, raw []byte) error
	SaveInventoryRaw(cid int, raw []byte) error
	StackableCatalog() []shared.EquipmentCatalogItem
	StoreTitle(uid int, rc robotconfig.RuntimeConfig) string
}

func (p Preparer) PopulateInventory(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	return p.PopulateInventoryFromCatalog(info, rc, p.Env.StackableCatalog())
}

func (p Preparer) PopulateInventoryFromCatalog(info robotcap.Info, rc robotconfig.RuntimeConfig, catalog []shared.EquipmentCatalogItem) error {
	env := p.Env
	plan := InventoryPlanFor(rc.StoreInventoryStartBox)
	items := p.selectItemsForPlan(info, rc, plan, catalog)
	if len(items) == 0 {
		return nil
	}
	invRaw, err := env.LoadInventory(info.CID)
	if err != nil || len(invRaw) < 249*61 {
		invRaw = make([]byte, 249*61)
	}
	for _, startBox := range InventoryClearStartBoxes(plan.StartBox) {
		for slot := 0; slot < rc.StoreItemSlots && slot < 24; slot++ {
			boxIndex := startBox + slot
			for _, rawIndex := range []int{boxIndex, boxIndex + 1, boxIndex + 2, boxIndex + 3} {
				if rawIndex >= 0 && rawIndex < 249 {
					clear(invRaw[rawIndex*61 : (rawIndex+1)*61])
				}
			}
		}
	}
	for slot, item := range items {
		boxIndex := plan.StartBox + slot
		rawIndex := boxIndex + 2
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		count := env.RandBetween(rc.StoreItemCountMin, rc.StoreItemCountMax)
		if count <= 0 {
			count = 1
		}
		WriteInventoryStack(invRaw[rawIndex*61:(rawIndex+1)*61], item, count, InventoryTypeForStackable(item, InventoryTypeForBoxIndex(boxIndex)))
	}
	env.Logf("[StorePrepare] uid=%d cid=%d store_plan=%s selected_items=%d slots=%d start_box=%d capacity=%d\n",
		info.UID, info.CID, plan.Name, len(items), rc.StoreItemSlots, plan.StartBox, rc.InventoryCapacity)
	if err := env.SaveInventory(info.CID, rc.InventoryCapacity, invRaw); err != nil {
		return err
	}
	p.WorldHorns.Invalidate(info.CID)
	return nil
}

func (p Preparer) EnsureInventoryAndStall(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	env := p.Env
	if err := p.EnsureStorePermission(info.UID, info.CID); err != nil {
		return err
	}
	if p.Pool != nil && (len(p.Pool.Materials) > 0 || len(p.Pool.Equipment) > 0) {
		return p.preparePoolInventoryAndStall(info, rc)
	}
	if err := p.PopulateInventory(info, rc); err != nil {
		return err
	}
	invRaw, err := env.LoadInventory(info.CID)
	if err != nil || len(invRaw) < 249*61 {
		return fmt.Errorf("inventory not found for cid=%d", info.CID)
	}
	var foundItems []int
	var stallItems []StallItem
	plan := InventoryPlanFor(rc.StoreInventoryStartBox)
	for slot := 0; slot < rc.StoreItemSlots && slot < 24; slot++ {
		boxIndex := plan.StartBox + slot
		rawIndex := boxIndex + 2
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		slotData := invRaw[rawIndex*61 : (rawIndex+1)*61]
		boxType := int(binary.BigEndian.Uint16(slotData[0:2]))
		itemID := int(binary.LittleEndian.Uint32(slotData[2:6]))
		count := int(binary.LittleEndian.Uint32(slotData[7:11]))
		if boxType > 0 && itemID > 0 && count > 0 {
			stallItems = append(stallItems, StallItem{ItemID: itemID, Count: count})
			foundItems = append(foundItems, itemID)
		}
	}
	if len(foundItems) == 0 {
		env.Logf("[StorePrepare] uid=%d cid=%d store_plan=%s inventory_found=0\n", info.UID, info.CID, plan.Name)
		return nil
	}
	assignStorePoolPrices(env, rc, stallItems, nil)
	title := env.StoreTitle(info.UID, rc)
	stallResult, err := env.ReplaceStoreStall(info.UID, title, stallItems)
	if err != nil {
		return err
	}
	env.Logf("[StorePrepare] uid=%d cid=%d store_plan=%s inventory_found=%d items=%v stall_rows=%d cfg_rows=%d title=%s\n",
		info.UID, info.CID, plan.Name, len(foundItems), foundItems, stallResult.StallRows, stallResult.ConfigRows, title)
	return nil
}

func (p Preparer) preparePoolInventoryAndStall(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	env := p.Env
	invRaw, err := env.LoadInventory(info.CID)
	if err != nil || len(invRaw) < 249*61 {
		invRaw = make([]byte, 249*61)
	}
	// Equipment box indexes are encoded two positions after the configured bag
	// index. Material positions are already global inventory indexes. Clear the
	// two legacy +2 material positions as well so an older preparation cannot
	// leave duplicate stacks behind.
	clearInventoryRawRange(invRaw, rc.StoreEquipmentStartBox+2, StoreEquipmentSlots)
	clearInventoryRawRange(invRaw, rc.StoreMaterialStartBox, StoreMaterialSlots+2)

	materials, equipment := p.Pool.Draw(info.UID)
	stallItems := make([]StallItem, 0, len(materials)+len(equipment))
	materialIndex := 0
	for _, entry := range materials {
		rawIndex := rc.StoreMaterialStartBox + materialIndex
		materialIndex++
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		count := storeMaterialCount(entry.Item)
		WriteInventoryStack(invRaw[rawIndex*61:(rawIndex+1)*61], entry.Item, count, 3)
		stallItems = append(stallItems, StallItem{ItemID: entry.Item.ID, Count: count})
	}
	materialRows := len(stallItems)
	for index, entry := range equipment {
		rawIndex := rc.StoreEquipmentStartBox + index + 2
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		copy(invRaw[rawIndex*61:(rawIndex+1)*61], entry.SlotBytes[:])
		stallItems = append(stallItems, StallItem{ItemID: entry.Item.ID, Count: 1})
	}
	if len(stallItems) == 0 {
		env.Logf("[StorePrepare] uid=%d cid=%d pool_empty=1\n", info.UID, info.CID)
		return nil
	}
	assignStorePoolPrices(env, rc, stallItems[:materialRows], stallItems[materialRows:])
	if err := env.SaveInventory(info.CID, rc.InventoryCapacity, invRaw); err != nil {
		return err
	}
	p.WorldHorns.Invalidate(info.CID)
	title := env.StoreTitle(info.UID, rc)
	result, err := env.ReplaceStoreStall(info.UID, title, stallItems)
	if err != nil {
		return err
	}
	env.Logf("[StorePrepare] uid=%d cid=%d pool_material=%d pool_equipment=%d stall_rows=%d title=%s\n",
		info.UID, info.CID, materialIndex, len(equipment), result.StallRows, title)
	return nil
}

func storeMaterialCount(item shared.EquipmentCatalogItem) int {
	// Prefer the PVF-defined stack limit. Some archives omit the field or
	// export an invalid value; those materials use the requested safe default.
	count := item.StackLimit
	if count <= 0 || count > 1000 {
		count = 1000
	}
	return count
}

func clearInventoryRawRange(raw []byte, start, count int) {
	for index := 0; index < count; index++ {
		rawIndex := start + index
		if rawIndex >= 0 && rawIndex < 249 && (rawIndex+1)*61 <= len(raw) {
			clear(raw[rawIndex*61 : (rawIndex+1)*61])
		}
	}
}

func assignStorePoolPrices(env PreparationEnv, rc robotconfig.RuntimeConfig, materials, equipment []StallItem) {
	assignStorePrices(env, materials, rc.StoreMaterialPriceMin, rc.StoreMaterialPriceMax)
	assignStorePrices(env, equipment, rc.StoreEquipmentPriceMin, rc.StoreEquipmentPriceMax)

	totalPrice := storeItemsTotalPrice(materials) + storeItemsTotalPrice(equipment)
	if totalPrice <= StoreTotalPriceLimit {
		return
	}
	scaleStorePrices(materials, totalPrice)
	scaleStorePrices(equipment, totalPrice)
}

func assignStorePrices(env PreparationEnv, items []StallItem, minPrice, maxPrice int) {
	for index := range items {
		price := env.RandBetween(minPrice, maxPrice)
		if price <= 0 {
			price = 1
		}
		items[index].Price = price
	}
}

func scaleStorePrices(items []StallItem, totalPrice int64) {
	for index := range items {
		price := int(int64(items[index].Price) * StoreTotalPriceLimit / totalPrice)
		if price <= 0 {
			price = 1
		}
		items[index].Price = price
	}
}

func storeItemsTotalPrice(items []StallItem) int64 {
	total := int64(0)
	for _, item := range items {
		count := item.Count
		if count <= 0 {
			count = 1
		}
		total += int64(item.Price) * int64(count)
	}
	return total
}

func (p Preparer) EnsureStorePermission(uid, cid int) error {
	env := p.Env
	status, err := env.EnsureStorePermissionRecord(uid, cid)
	if err != nil {
		return err
	}
	env.Logf("[StorePrepare] uid=%d cid=%d permission premium=%d miles=%d prod_user=%d pu_user=%d event_entry=%d\n",
		uid, cid, status.Premium, status.Miles, status.ProdUser, status.PUUser, status.EventEntry)
	return nil
}

func (p Preparer) EnsureWorldHornByCID(cid int) error {
	return p.WorldHorns.Ensure(cid, func() error {
		return p.ensureWorldHornByCID(cid)
	})
}

func (p Preparer) ensureWorldHornByCID(cid int) error {
	invRaw, err := p.Env.LoadInventory(cid)
	if err != nil {
		return fmt.Errorf("world horn inventory cid=%d: %w", cid, err)
	}
	if len(invRaw) < 249*61 {
		return fmt.Errorf("world horn inventory blob is too short")
	}
	slot := invRaw[WorldHornRawIndex*61 : (WorldHornRawIndex+1)*61]
	itemID := int(binary.LittleEndian.Uint32(slot[2:6]))
	count := int(binary.LittleEndian.Uint32(slot[7:11]))
	if int(binary.BigEndian.Uint16(slot[0:2])) == InventoryTypeForBoxIndex(WorldHornBoxIndex) && itemID == WorldHornItemID && count > 0 {
		return nil
	}
	WriteInventoryStack(slot, shared.EquipmentCatalogItem{ID: WorldHornItemID}, WorldHornCount, InventoryTypeForBoxIndex(WorldHornBoxIndex))
	if err := p.Env.SaveInventoryRaw(cid, invRaw); err != nil {
		return fmt.Errorf("update world horn inventory cid=%d: %w", cid, err)
	}
	return nil
}

func (p Preparer) selectItemsForPlan(info robotcap.Info, rc robotconfig.RuntimeConfig, plan InventoryPlan, catalog []shared.EquipmentCatalogItem) []shared.EquipmentCatalogItem {
	count := rc.StoreItemSlots
	if count <= 0 {
		count = 6
	}
	if count > 24 {
		count = 24
	}
	rng := rand.New(rand.NewSource(int64(p.Env.RandBetween(0, 1<<30)) ^ int64(info.UID)<<32))
	preferred := newItemReservoir(count, rng)
	basic := newItemReservoir(count, rng)
	fallback := newItemReservoir(count, rng)
	wantSlot := "material"
	if InventoryTypeForBoxIndex(plan.StartBox) == 2 {
		wantSlot = "waste"
	}
	for _, item := range catalog {
		if item.ID <= 0 || item.Expire {
			continue
		}
		if item.NoTrade || item.NeedMaterial || item.BadName {
			continue
		}
		if item.CanTrade != nil && !*item.CanTrade {
			continue
		}
		if item.Level > 0 && item.Level > info.Level {
			continue
		}
		if item.StackLimit == 1 {
			continue
		}
		if !strings.EqualFold(item.Slot, wantSlot) {
			continue
		}
		if wantSlot == "material" {
			icon := strings.ToLower(item.Icon)
			if item.FieldImage == "" || !strings.Contains(icon, "material.img") {
				continue
			}
		}
		if item.Trade || AttachPreferred(item.Attach) {
			preferred.Add(item)
			if item.BasicMaterial {
				basic.Add(item)
			}
			continue
		}
		if AttachAllowed(item.Attach) {
			fallback.Add(item)
		}
	}
	candidates := preferred.Items()
	if basic.Seen() > 0 {
		candidates = basic.Items()
	} else if preferred.Seen() == 0 {
		candidates = fallback.Items()
	}
	return candidates
}

type itemReservoir struct {
	capacity int
	seen     int
	items    []shared.EquipmentCatalogItem
	rng      *rand.Rand
}

func newItemReservoir(capacity int, rng *rand.Rand) *itemReservoir {
	return &itemReservoir{capacity: capacity, items: make([]shared.EquipmentCatalogItem, 0, capacity), rng: rng}
}

func (r *itemReservoir) Add(item shared.EquipmentCatalogItem) {
	if r == nil || r.capacity <= 0 || r.rng == nil {
		return
	}
	r.seen++
	if len(r.items) < r.capacity {
		r.items = append(r.items, item)
		return
	}
	if index := r.rng.Intn(r.seen); index < r.capacity {
		r.items[index] = item
	}
}

func (r *itemReservoir) Seen() int {
	if r == nil {
		return 0
	}
	return r.seen
}

func (r *itemReservoir) Items() []shared.EquipmentCatalogItem {
	if r == nil || len(r.items) == 0 {
		return nil
	}
	r.rng.Shuffle(len(r.items), func(i, j int) { r.items[i], r.items[j] = r.items[j], r.items[i] })
	return r.items
}
