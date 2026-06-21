package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	worldHornItemID   = 36
	worldHornCount    = 200
	worldHornBoxIndex = 55
	worldHornRawIndex = worldHornBoxIndex + 2
)

func (m *RobotManager) populateStoreInventory(info RobotInfo, rc robotRuntimeConfig) error {
	items := m.selectStoreItems(info, rc)
	if len(items) == 0 {
		return nil
	}
	var invRaw []byte
	row := m.db.QueryRow("SELECT UNCOMPRESS(inventory) FROM taiwan_cain_2nd.inventory WHERE charac_no=?", info.CID)
	if err := row.Scan(&invRaw); err != nil || len(invRaw) < 249*61 {
		invRaw = make([]byte, 249*61)
	}
	for slot := 0; slot < rc.StoreItemSlots && slot < 24; slot++ {
		boxIndex := rc.StoreInventoryStartBox + slot
		for _, rawIndex := range []int{boxIndex, boxIndex + 1, boxIndex + 2, boxIndex + 3} {
			if rawIndex >= 0 && rawIndex < 249 {
				clear(invRaw[rawIndex*61 : (rawIndex+1)*61])
			}
		}
	}
	for slot, item := range items {
		boxIndex := rc.StoreInventoryStartBox + slot
		rawIndex := boxIndex + 2
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		count := m.randBetween(rc.StoreItemCountMin, rc.StoreItemCountMax)
		if count <= 0 {
			count = 1
		}
		writeInventoryStack(invRaw[rawIndex*61:(rawIndex+1)*61], item, count, inventoryTypeForBoxIndex(boxIndex))
	}
	_, err := m.db.Exec("UPDATE taiwan_cain_2nd.inventory SET inventory_capacity=?,inventory=? WHERE charac_no=?", rc.InventoryCapacity, compressRaw(invRaw), info.CID)
	return err
}

func (m *RobotManager) ensureStoreInventoryAndStall(r RobotInfo, rc robotRuntimeConfig) error {
	if err := m.ensureStorePermission(r.UID, r.CID); err != nil {
		return err
	}
	if err := m.populateStoreInventory(r, rc); err != nil {
		return err
	}
	var invRaw []byte
	row := m.db.QueryRow("SELECT UNCOMPRESS(inventory) FROM taiwan_cain_2nd.inventory WHERE charac_no=?", r.CID)
	if err := row.Scan(&invRaw); err != nil || len(invRaw) < 249*61 {
		return fmt.Errorf("inventory not found for cid=%d", r.CID)
	}
	_, _ = m.db.Exec("DELETE FROM d_starsky.Robot_stall WHERE UID=? AND function_type=2", r.UID)
	var foundItems []int
	for slot := 0; slot < rc.StoreItemSlots && slot < 24; slot++ {
		boxIndex := rc.StoreInventoryStartBox + slot
		rawIndex := boxIndex + 2
		if rawIndex < 0 || rawIndex >= 249 {
			continue
		}
		slotData := invRaw[rawIndex*61 : (rawIndex+1)*61]
		boxType := int(binary.BigEndian.Uint16(slotData[0:2]))
		itemID := int(binary.LittleEndian.Uint32(slotData[2:6]))
		count := int(binary.LittleEndian.Uint32(slotData[7:11]))
		if boxType > 0 && itemID > 0 && count > 0 {
			price := m.randBetween(rc.StorePriceMin, rc.StorePriceMax)
			if price <= 0 {
				price = 100000
			}
			if _, err := m.db.Exec("INSERT INTO d_starsky.Robot_stall (Trade_item,price,item_number,function_type,state,UID) VALUES (?,?,?,?,1,?)", itemID, price, count, 2, r.UID); err != nil {
				return err
			}
			foundItems = append(foundItems, itemID)
		}
	}
	if len(foundItems) == 0 {
		return nil
	}
	title := fmt.Sprintf("tw-%d", r.UID%100000)
	_, _ = m.db.Exec("DELETE FROM d_starsky.Robot_stall_config WHERE UID=? AND function_type=2 AND cfg_type=3", r.UID)
	_, _ = m.db.Exec("INSERT INTO d_starsky.Robot_stall_config (cfg_content,cfg_type,UID,function_type,state) VALUES (?,3,?,2,1)", title, r.UID)
	return nil
}

func (m *RobotManager) ensureRobotWorldHorn(uid int) error {
	var cid int
	if err := m.db.QueryRow("SELECT cid FROM d_starsky.robot_registry WHERE uid=? LIMIT 1", uid).Scan(&cid); err != nil {
		return fmt.Errorf("world horn robot uid=%d not registered: %w", uid, err)
	}
	return m.ensureRobotWorldHornByCID(cid)
}

func (m *RobotManager) ensureRobotWorldHornByCID(cid int) error {
	var invRaw []byte
	row := m.db.QueryRow("SELECT UNCOMPRESS(inventory) FROM taiwan_cain_2nd.inventory WHERE charac_no=?", cid)
	if err := row.Scan(&invRaw); err != nil {
		return fmt.Errorf("world horn inventory cid=%d: %w", cid, err)
	}
	if len(invRaw) < 249*61 {
		return errors.New("world horn inventory blob is too short")
	}
	slot := invRaw[worldHornRawIndex*61 : (worldHornRawIndex+1)*61]
	itemID := int(binary.LittleEndian.Uint32(slot[2:6]))
	count := int(binary.LittleEndian.Uint32(slot[7:11]))
	if int(binary.BigEndian.Uint16(slot[0:2])) == inventoryTypeForBoxIndex(worldHornBoxIndex) && itemID == worldHornItemID && count >= 1 {
		return nil
	}
	clear(slot)
	writeInventoryStack(slot, equipmentCatalogItem{ID: worldHornItemID}, worldHornCount, inventoryTypeForBoxIndex(worldHornBoxIndex))
	if _, err := m.db.Exec("UPDATE taiwan_cain_2nd.inventory SET inventory=? WHERE charac_no=?", compressRaw(invRaw), cid); err != nil {
		return fmt.Errorf("update world horn inventory cid=%d: %w", cid, err)
	}
	return nil
}

func (m *RobotManager) selectStoreItems(r RobotInfo, rc robotRuntimeConfig) []equipmentCatalogItem {
	catalog := m.loadStackableCatalog()
	count := rc.StoreItemSlots
	if count <= 0 {
		count = 6
	}
	if count > 24 {
		count = 24
	}
	var candidates []equipmentCatalogItem
	var basicCandidates []equipmentCatalogItem
	var fallback []equipmentCatalogItem
	wantSlot := "material"
	if inventoryTypeForBoxIndex(rc.StoreInventoryStartBox) == 2 {
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
		if item.Level > 0 && item.Level > r.Level {
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
		if item.Trade || storeAttachPreferred(item.Attach) {
			candidates = append(candidates, item)
			if item.BasicMaterial {
				basicCandidates = append(basicCandidates, item)
			}
			continue
		}
		if storeAttachAllowed(item.Attach) {
			fallback = append(fallback, item)
		}
	}
	if len(candidates) == 0 {
		candidates = fallback
	}
	if len(basicCandidates) > 0 {
		candidates = basicCandidates
	}
	if len(candidates) == 0 {
		for _, id := range rc.StoreItemAllowIDs {
			if id > 0 && !intInSlice(rc.StoreItemDenyIDs, id) {
				candidates = append(candidates, equipmentCatalogItem{ID: id, Slot: wantSlot, Trade: true, StackLimit: 1000})
			}
		}
	}
	m.randShuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	if len(candidates) > count {
		candidates = candidates[:count]
	}
	return candidates
}

func storeAttachAllowed(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	if attach == "" {
		return false
	}
	if strings.Contains(attach, "account") || strings.Contains(attach, "creature") || strings.Contains(attach, "unable") || strings.Contains(attach, "not") {
		return false
	}
	return strings.Contains(attach, "trade") || attach == "free" || attach == "sealing"
}

func intInSlice(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func storeAttachPreferred(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	return attach == "trade" || strings.Contains(attach, "trade ") || attach == "free" || attach == "sealing"
}

func inventoryTypeForBoxIndex(boxIndex int) int {
	switch {
	case boxIndex >= 7 && boxIndex <= 54:
		return 1
	case boxIndex >= 55 && boxIndex <= 102:
		return 2
	case boxIndex >= 103 && boxIndex <= 150:
		return 3
	case boxIndex >= 151 && boxIndex <= 198:
		return 4
	case boxIndex >= 199 && boxIndex <= 246:
		return 10
	default:
		return 2
	}
}

func writeInventoryStack(dst []byte, item equipmentCatalogItem, count int, inventoryType int) {
	if len(dst) < 61 {
		return
	}
	dst[0] = 0x00
	dst[1] = byte(inventoryType)
	binary.LittleEndian.PutUint32(dst[2:6], uint32(item.ID))
	binary.LittleEndian.PutUint32(dst[7:11], uint32(count))
}
