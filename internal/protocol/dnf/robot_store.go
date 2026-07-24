package dnf

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	sqlpkg "robot/internal/foundation/sql"
)

const (
	storeQueryTimeout        = 3 * time.Second
	privateStoreDisplayLimit = 7
)

type StoreInfo struct {
	Index    int
	ItemID   int
	BoxType  int
	BoxIndex int
	Price    int
	Count    int
}

type Transaction struct {
	ItemPos  int16
	ItemId   int32
	ItemNum  int32
	ItemType int32
}

func (r *RobotVo) ResetPrivateStoreState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RobotTyp == 2 {
		r.RobotTyp = 0
	}
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.StoreCreateRejected = false
	r.LastStoreError = 0
	r.StoreCreated = false
	r.PendingStoreTitle = ""
	r.LastStoreDisplay = nil
	r.storeDisplayCandidates = nil
	r.storeDisplayRetryPrefix = 0
	r.storeDisplayRetrySingle = 0
}

func (r *RobotVo) ResetDisjointStoreState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RobotTyp == 3 {
		r.RobotTyp = 0
	}
	r.DisjointCreateSent = false
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
}

func (r *RobotVo) PreparePrivateStoreState(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PendingStoreTitle = title
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.StoreCreateRejected = false
	r.LastStoreError = 0
	r.StoreCreated = false
	r.LastStoreDisplay = nil
	r.storeDisplayCandidates = nil
	r.storeDisplayRetryPrefix = 0
	r.storeDisplayRetrySingle = 0
	r.RobotTyp = 2
}

func (r *RobotVo) OpenDisjointStore(cost uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}

	var openDisjoint [16]byte
	openDisjoint[0] = 0x01
	openDisjoint[4] = 0x01
	binary.LittleEndian.PutUint32(openDisjoint[5:9], cost)
	binary.LittleEndian.PutUint16(openDisjoint[9:11], r.CurX)
	binary.LittleEndian.PutUint16(openDisjoint[11:13], r.CurY)

	pkt, err := buildSendPacket(238, uint16(r.PacketID), openDisjoint[:], r.Cipher)
	r.PacketID++
	if err != nil || !r.SendMsg(pkt) {
		return false
	}
	r.RobotTyp = 3
	r.DisjointCreateSent = true
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
	fmt.Printf("[DISJOINT_238_SENT] uid=%d cost=%d x=%d y=%d\n", r.UID, cost, r.CurX, r.CurY)
	return true
}

func (r *RobotVo) CreatePrivateStore() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}
	r.StoreCreateRejected = false

	var data [16]byte
	data[6] = 0xFF
	data[7] = 0xFF
	data[0] = r.CurVillage
	data[1] = r.CurArea
	binary.LittleEndian.PutUint16(data[2:4], r.CurX)
	binary.LittleEndian.PutUint16(data[4:6], r.CurY)
	pkt, err := buildSendPacket(88, uint16(r.PacketID), data[:], r.Cipher)
	if err != nil || !r.SendMsg(pkt) {
		r.StoreCreateRejected = true
		r.LastStoreError = 0
		return false
	}
	r.PacketID++
	r.RobotTyp = 2
	return true
}

func (r *RobotVo) CompleteDisplay(title string, storeInfo []StoreInfo) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeDisplay(title, storeInfo)
}

func (r *RobotVo) completeDisplay(title string, storeInfo []StoreInfo) bool {
	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}
	if r.StoreDisplaySent {
		return false
	}
	if len(storeInfo) == 0 {
		return false
	}
	if len(r.storeDisplayCandidates) == 0 {
		r.storeDisplayCandidates = append([]StoreInfo(nil), storeInfo...)
		r.storeDisplayRetryPrefix = len(storeInfo) - 1
		r.storeDisplayRetrySingle = 1
	}

	realSize := 4 + len(title) + 1 + len(storeInfo)*13 + 2
	alinSize := alignTo(realSize, 8)
	data := make([]byte, alinSize)

	binary.LittleEndian.PutUint32(data[0:4], uint32(len(title)))
	copy(data[4:], []byte(title))
	data[4+len(title)] = byte(len(storeInfo))

	pBuf := data[4+len(title)+1:]
	for i, si := range storeInfo {
		off := i * 13
		binary.LittleEndian.PutUint16(pBuf[off+0:off+2], uint16(si.Index))
		binary.LittleEndian.PutUint32(pBuf[off+2:off+6], uint32(si.Price))
		pBuf[off+6] = byte(si.BoxType)
		binary.LittleEndian.PutUint16(pBuf[off+7:off+9], uint16(si.BoxIndex))
		binary.LittleEndian.PutUint32(pBuf[off+9:off+13], uint32(si.Count))
	}
	endOff := 4 + len(title) + 1 + len(storeInfo)*13
	pBuf = data[endOff:]
	if len(pBuf) >= 2 {
		pBuf[0] = 0xFF
		pBuf[1] = 0xFF
	}

	pkt, err := buildSendPacket(90, uint16(r.PacketID), data, r.Cipher)
	if err != nil || !r.SendMsg(pkt) {
		r.StoreDisplayRejected = true
		r.LastStoreError = 0
		return false
	}
	r.PacketID++
	r.StoreDisplaySent = true
	r.StoreDisplayAck = false
	r.LastStoreDisplay = append(r.LastStoreDisplay[:0], storeInfo...)
	fmt.Printf("[STORE_90_SENT] uid=%d items=%d list=%+v\n", r.UID, len(storeInfo), storeInfo)
	return true
}

// retryPrivateStoreDisplayUnsafe retries CMD 90 after the legacy server has
// rejected a slot with 0x11. The server resets its temporary item list on that
// error but keeps the store in the created state, so another display packet can
// be sent on the same connection. Prefer the largest prefix first (which keeps
// most goods when trailing cached slots are empty), then probe the remaining
// individual slots so any valid item can still open the stall.
func (r *RobotVo) retryPrivateStoreDisplayUnsafe() bool {
	candidates := r.storeDisplayCandidates
	if len(candidates) <= 1 {
		return false
	}
	var next []StoreInfo
	mode := ""
	if r.storeDisplayRetryPrefix >= 1 {
		count := r.storeDisplayRetryPrefix
		r.storeDisplayRetryPrefix--
		next = append([]StoreInfo(nil), candidates[:count]...)
		mode = "prefix"
	} else if r.storeDisplayRetrySingle < len(candidates) {
		index := r.storeDisplayRetrySingle
		r.storeDisplayRetrySingle++
		next = []StoreInfo{candidates[index]}
		mode = "single"
	} else {
		return false
	}
	for index := range next {
		next[index].Index = index
	}
	r.StoreDisplaySent = false
	r.StoreDisplayRejected = false
	r.LastStoreError = 0
	fmt.Printf("[STORE_90_RETRY] uid=%d mode=%s items=%d list=%+v\n", r.UID, mode, len(next), next)
	return r.completeDisplay(r.PendingStoreTitle, next)
}

func (r *RobotVo) GetCompleteDisplay(flag int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}
	r.IsWaitingItemList = true
	var data [8]byte
	data[0] = byte(flag)
	pkt, err := buildSendPacket(20, uint16(r.PacketID), data[:], r.Cipher)
	if err != nil || !r.SendMsg(pkt) {
		r.IsWaitingItemList = false
		r.StoreDisplayRejected = true
		r.LastStoreError = 0
		return false
	}
	r.PacketID++
	return true
}

func (r *RobotVo) PrivateStoreItemListReceived() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.IsWaitingItemList
}

func (r *RobotVo) MarkPrivateStoreCreateFailed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.StoreCreated || r.StoreCreateRejected {
		return
	}
	r.StoreCreateRejected = true
	r.LastStoreError = 0
}

func (r *RobotVo) MarkPrivateStoreDisplayFailed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.StoreDisplaySent || r.StoreDisplayAck || r.StoreDisplayRejected {
		return
	}
	r.StoreDisplayRejected = true
	r.LastStoreError = 0
}

func (r *RobotVo) GetDbDataAndCompleteDisplay() bool {
	r.mu.Lock()
	if r.State != StateRun || r.partyActiveUnsafe() || !r.StoreCreated || r.DB == nil {
		r.mu.Unlock()
		return false
	}
	uid := r.UID
	cid := r.CID
	db := r.DB
	title := r.PendingStoreTitle
	inventoryVersion := r.storeInventoryVersion
	inventory := make(map[int]Transaction, len(r.InfanMap))
	for itemID, transaction := range r.InfanMap {
		inventory[itemID] = transaction
	}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeQueryTimeout)
	defer cancel()
	// CMD 20/CMD 13 reports stackable bags on this legacy build but omits
	// singleton equipment. Merge equipment records from the persisted inventory;
	// reconciliation below still accepts only IDs present in the prepared stall.
	var invRaw []byte
	if err := db.QueryRowContext(ctx, "SELECT UNCOMPRESS(inventory) FROM taiwan_cain_2nd.inventory WHERE charac_no=?", cid).Scan(&invRaw); err == nil {
		for rawIndex := 2; (rawIndex+1)*61 <= len(invRaw); rawIndex++ {
			slot := invRaw[rawIndex*61 : (rawIndex+1)*61]
			// Byte 0 is the sealed-instance flag; byte 1 is the inventory
			// type. Sealed equipment therefore starts with 01 01, not 00 01.
			if slot[1] != 1 {
				continue
			}
			itemID := int32(binary.LittleEndian.Uint32(slot[2:6]))
			if itemID > 0 {
				boxIndex := rawIndex
				inventory[boxIndex] = Transaction{ItemPos: int16(boxIndex), ItemId: itemID, ItemNum: 0}
			}
		}
	}
	rows, err := sqlpkg.SelectContext(ctx, db, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID", uid)
	if err != nil {
		return false
	}

	storeInfo := reconcileStoreDisplay(rows, inventory)

	if len(storeInfo) > 0 {
		customTitle := title != ""
		if title == "" {
			title = "store"
		}

		cfgRows, _ := sqlpkg.SelectContext(ctx, db, "select cfg_content from d_starsky.Robot_stall_config where cfg_type=3 and function_type=2 and state=1 and (UID=? or UID=0) order by UID", uid)
		if len(cfgRows) > 0 && len(cfgRows[0]) > 0 && !customTitle {
			title = cfgRows[0][0]
		}

		r.mu.Lock()
		if r.State == StateRun && r.StoreCreated && r.UID == uid && r.DB == db && r.storeInventoryVersion == inventoryVersion {
			r.completeDisplay(title, storeInfo)
		}
		r.mu.Unlock()
	}

	return true
}

func reconcileStoreDisplay(rows [][]string, inventory map[int]Transaction) []StoreInfo {
	storeInfo := make([]StoreInfo, 0, min(len(rows), privateStoreDisplayLimit))
	usedSlots := make(map[int16]struct{}, len(inventory))
	for _, row := range rows {
		if len(storeInfo) >= privateStoreDisplayLimit || len(row) < 3 || row[0] == "" || row[1] == "" || row[2] == "" {
			continue
		}
		tradeItem, errItem := strconv.Atoi(row[0])
		price, errPrice := strconv.Atoi(row[1])
		wantedCount, errCount := strconv.Atoi(row[2])
		if errItem != nil || errPrice != nil || errCount != nil || price <= 0 || wantedCount <= 0 {
			continue
		}

		var selected Transaction
		found := false
		for _, tx := range inventory {
			if int(tx.ItemId) != tradeItem || (tx.ItemNum <= 0 && wantedCount > 1) {
				continue
			}
			if _, used := usedSlots[tx.ItemPos]; used {
				continue
			}
			if !found || tx.ItemNum > selected.ItemNum {
				selected = tx
				found = true
			}
		}
		if !found {
			continue
		}
		count := wantedCount
		if selected.ItemNum <= 0 {
			// CMD 13 reports singleton equipment with quantity zero, while CMD 90
			// requires every non-special item to carry a positive sale quantity.
			// Selling one transfers the complete equipment instance from this slot.
			count = 1
		} else if available := int(selected.ItemNum); count > available {
			count = available
		}
		if count < 0 {
			continue
		}
		usedSlots[selected.ItemPos] = struct{}{}
		storeInfo = append(storeInfo, StoreInfo{
			Index:  len(storeInfo),
			ItemID: tradeItem,
			// CMD 90 uses the legacy normal-inventory item space (0) for all
			// global bag indexes on this server.  The index itself selects the
			// equipment/material range; sending 7 for material makes this build
			// reject otherwise valid items with 0x11.
			BoxType:  0,
			BoxIndex: int(selected.ItemPos),
			Price:    price,
			Count:    count,
		})
	}
	return storeInfo
}

func (r *RobotVo) CompleteDisplayFromStallFallback() bool {
	r.mu.Lock()
	if r.State != StateRun || r.partyActiveUnsafe() || !r.StoreCreated || r.StoreDisplaySent || r.StoreDisplayAck || r.StoreDisplayRejected || r.DB == nil {
		r.mu.Unlock()
		return false
	}
	uid := r.UID
	title := r.PendingStoreTitle
	db := r.DB
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeQueryTimeout)
	defer cancel()
	rows, err := sqlpkg.SelectContext(ctx, db, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID,id limit 14", uid)
	if err != nil || len(rows) == 0 {
		return false
	}

	type invPos struct {
		BoxType      int
		RawBoxIndex  int
		GameBoxIndex int
		Count        int
	}
	inventory := make(map[int]invPos)
	var invRaw []byte
	err = db.QueryRowContext(ctx, "SELECT UNCOMPRESS(i.inventory) FROM taiwan_cain.charac_info c JOIN taiwan_cain_2nd.inventory i ON i.charac_no=c.charac_no WHERE c.m_id=? ORDER BY c.charac_no LIMIT 1", uid).Scan(&invRaw)
	if err == nil {
		for rawIndex := 0; rawIndex+1 < 249 && (rawIndex+1)*61 <= len(invRaw); rawIndex++ {
			slot := invRaw[rawIndex*61 : (rawIndex+1)*61]
			boxType := int(slot[1])
			itemID := int(binary.LittleEndian.Uint32(slot[2:6]))
			count := int(binary.LittleEndian.Uint32(slot[7:11]))
			if boxType > 0 && itemID > 0 {
				gameIndex := rawIndex
				pos := invPos{BoxType: boxType, RawBoxIndex: rawIndex, GameBoxIndex: gameIndex, Count: count}
				if old, ok := inventory[itemID]; !ok || (!isStoreStackableType(old.BoxType) && isStoreStackableType(boxType)) {
					inventory[itemID] = pos
				}
			}
		}
	}

	type storeRow struct {
		ItemID int
		Price  int
		Count  int
		Pos    invPos
	}
	storeRows := make([]storeRow, 0, len(rows))
	for _, row := range rows {
		if len(row) < 3 || row[1] == "" || row[2] == "" {
			continue
		}
		tradeItem, _ := strconv.Atoi(row[0])
		price, _ := strconv.Atoi(row[1])
		itemNumber, _ := strconv.Atoi(row[2])
		if price <= 0 || itemNumber <= 0 {
			continue
		}
		pos, ok := inventory[tradeItem]
		if !ok {
			continue
		}
		if pos.Count > 0 && itemNumber > pos.Count {
			itemNumber = pos.Count
		}
		storeRows = append(storeRows, storeRow{ItemID: tradeItem, Price: price, Count: itemNumber, Pos: pos})
	}
	if len(storeRows) == 0 {
		return false
	}
	if title == "" {
		title = "store"
	}

	r.mu.Lock()
	if r.State != StateRun || r.partyActiveUnsafe() || !r.StoreCreated || r.StoreDisplaySent || r.StoreDisplayAck || r.StoreDisplayRejected || r.UID != uid || r.DB != db {
		sent := r.StoreDisplaySent || r.StoreDisplayAck
		r.mu.Unlock()
		return sent
	}
	r.StoreDisplayRejected = false
	storeInfo := make([]StoreInfo, 0, len(storeRows))
	for i, sr := range storeRows {
		count := sr.Count
		if !isStoreStackableType(sr.Pos.BoxType) {
			count = 1
		} else if count <= 0 {
			continue
		}
		storeInfo = append(storeInfo, StoreInfo{Index: i, ItemID: sr.ItemID, BoxType: 0, BoxIndex: sr.Pos.GameBoxIndex, Price: sr.Price, Count: count})
	}
	sent := r.completeDisplay(title, storeInfo)
	r.mu.Unlock()
	return sent
}

func isStoreStackableType(itemType int) bool {
	switch itemType {
	case 2, 3, 4, 10:
		return true
	default:
		return false
	}
}
