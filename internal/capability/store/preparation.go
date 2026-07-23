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
}

type PreparationEnv interface {
	EnsureStorePermissionRecord(uid, cid int) (PermissionStatus, error)
	LoadInventory(cid int) ([]byte, error)
	Logf(format string, args ...interface{})
	RandBetween(min, max int) int
	ReplaceStoreStall(uid int, title string, items []StallItem) (StallResult, error)
	RobotCID(uid int) (int, error)
	SaveInventory(cid int, capacity int, raw []byte) error
	SaveInventoryRaw(cid int, raw []byte) error
	StackableCatalog() []shared.EquipmentCatalogItem
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
			price := env.RandBetween(rc.StorePriceMin, rc.StorePriceMax)
			if price <= 0 {
				price = 100000
			}
			stallItems = append(stallItems, StallItem{ItemID: itemID, Count: count, Price: price})
			foundItems = append(foundItems, itemID)
		}
	}
	if len(foundItems) == 0 {
		env.Logf("[StorePrepare] uid=%d cid=%d store_plan=%s inventory_found=0\n", info.UID, info.CID, plan.Name)
		return nil
	}
	title := fmt.Sprintf("tw-%d", info.UID%100000)
	stallResult, err := env.ReplaceStoreStall(info.UID, title, stallItems)
	if err != nil {
		return err
	}
	env.Logf("[StorePrepare] uid=%d cid=%d store_plan=%s inventory_found=%d items=%v stall_rows=%d cfg_rows=%d title=%s\n",
		info.UID, info.CID, plan.Name, len(foundItems), foundItems, stallResult.StallRows, stallResult.ConfigRows, title)
	return nil
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

func (p Preparer) EnsureWorldHorn(uid int) error {
	cid, err := p.Env.RobotCID(uid)
	if err != nil {
		return fmt.Errorf("world horn robot uid=%d not registered: %w", uid, err)
	}
	return p.EnsureWorldHornByCID(cid)
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

func (p Preparer) SelectItems(info robotcap.Info, rc robotconfig.RuntimeConfig) []shared.EquipmentCatalogItem {
	return p.selectItemsForPlan(info, rc, InventoryPlanFor(rc.StoreInventoryStartBox), p.Env.StackableCatalog())
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
		if len(rc.StoreItemAllowIDs) > 0 && !intInSlice(rc.StoreItemAllowIDs, item.ID) {
			continue
		}
		if intInSlice(rc.StoreItemDenyIDs, item.ID) {
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
	if len(candidates) == 0 {
		allowed := newItemReservoir(count, rng)
		for _, id := range rc.StoreItemAllowIDs {
			if id > 0 && !intInSlice(rc.StoreItemDenyIDs, id) {
				allowed.Add(shared.EquipmentCatalogItem{ID: id, Slot: wantSlot, Trade: true, StackLimit: 1000})
			}
		}
		candidates = allowed.Items()
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

func intInSlice(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
