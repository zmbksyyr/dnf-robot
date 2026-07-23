package store

import (
	"math/rand"
	"sort"
	"strings"

	equipcap "robot/internal/capability/equipment"
	"robot/internal/shared"
)

const (
	// The legacy private-store window has 14 display slots in total. Keep the
	// two item classes balanced so every generated row can be displayed.
	StoreStackableSlots      = 7
	StoreEquipmentSlots      = 7
	StoreInventoryClearSlots = 12
	StoreStackFallback       = 2000
	StoreTotalPriceLimit     = 500000000
)

type PoolKind uint8

const (
	PoolConsumable PoolKind = iota + 1
	PoolMaterial
	PoolEquipment
)

type PoolEntry struct {
	Item      shared.EquipmentCatalogItem
	Kind      PoolKind
	MaxCount  int
	SlotBytes [61]byte
}

type ItemPool struct {
	Stackable []PoolEntry
	Equipment []PoolEntry
}

func BuildItemPool(equipment, stackable []shared.EquipmentCatalogItem, intensify int) *ItemPool {
	if intensify < 0 {
		intensify = 0
	}
	if intensify > 255 {
		intensify = 255
	}
	pool := &ItemPool{}
	seenStackable := make(map[int]struct{})
	for _, item := range stackable {
		kind, ok := stackablePoolKind(item)
		if !ok || !storeTradeAllowed(item) {
			continue
		}
		if _, exists := seenStackable[item.ID]; exists {
			continue
		}
		limit := item.StackLimit
		if limit == 1 {
			continue
		}
		if limit <= 0 {
			limit = StoreStackFallback
		}
		seenStackable[item.ID] = struct{}{}
		pool.Stackable = append(pool.Stackable, PoolEntry{Item: item, Kind: kind, MaxCount: limit})
	}

	seenEquipment := make(map[int]struct{})
	for _, item := range equipment {
		if item.ID <= 0 || item.ItemType < 1 || item.ItemType > 12 || item.Expire || item.NoTrade || item.TradeBlock {
			continue
		}
		if item.CanTrade != nil && !*item.CanTrade {
			continue
		}
		attach := strings.ToLower(strings.TrimSpace(item.Attach))
		if !strings.Contains(attach, "sealing") {
			continue
		}
		if _, exists := seenEquipment[item.ID]; exists {
			continue
		}
		seenEquipment[item.ID] = struct{}{}
		entry := PoolEntry{Item: item, Kind: PoolEquipment, MaxCount: 1}
		rng := rand.New(rand.NewSource(int64(item.ID)))
		equipcap.WriteStoreEquipSlot(entry.SlotBytes[:], item, rng, intensify)
		pool.Equipment = append(pool.Equipment, entry)
	}
	sort.Slice(pool.Stackable, func(i, j int) bool { return pool.Stackable[i].Item.ID < pool.Stackable[j].Item.ID })
	sort.Slice(pool.Equipment, func(i, j int) bool { return pool.Equipment[i].Item.ID < pool.Equipment[j].Item.ID })
	return pool
}

func (p *ItemPool) Draw(uid int) (stackable, equipment []PoolEntry) {
	if p == nil {
		return nil, nil
	}
	seed := int64(uid)*0x5deece66d + 0xb
	rng := rand.New(rand.NewSource(seed))
	return drawPoolEntries(p.Stackable, StoreStackableSlots, rng), drawPoolEntries(p.Equipment, StoreEquipmentSlots, rng)
}

func drawPoolEntries(source []PoolEntry, count int, rng *rand.Rand) []PoolEntry {
	if count <= 0 || len(source) == 0 {
		return nil
	}
	if count > len(source) {
		count = len(source)
	}
	out := make([]PoolEntry, 0, count)
	selected := make(map[int]struct{}, count)
	for len(out) < count {
		index := rng.Intn(len(source))
		if _, exists := selected[index]; exists {
			continue
		}
		selected[index] = struct{}{}
		out = append(out, source[index])
	}
	return out
}

func stackablePoolKind(item shared.EquipmentCatalogItem) (PoolKind, bool) {
	if item.ID <= 0 || item.Expire || item.NoTrade || item.TradeBlock {
		return 0, false
	}
	slot := strings.ToLower(strings.TrimSpace(item.Slot))
	switch {
	case strings.Contains(slot, "material"):
		return PoolMaterial, true
	default:
		return 0, false
	}
}

func storeTradeAllowed(item shared.EquipmentCatalogItem) bool {
	if item.CanTrade != nil && !*item.CanTrade {
		return false
	}
	attach := strings.ToLower(strings.TrimSpace(item.Attach))
	if attach == "" {
		return item.Trade
	}
	return attach == "trade" || attach == "free" || attach == "sealing" || attach == "sealing trade"
}
