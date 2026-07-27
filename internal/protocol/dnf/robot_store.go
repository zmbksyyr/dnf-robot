package dnf

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	r.storeDisplayRetryPlans = nil
	r.storeDisplayRetryIndex = 0
}

// QueueDisjointAfterRun mirrors the original successful disjoint workflow.
// Keeping the action on RobotVo avoids a scheduler race between observing
// StateRun and the game server completing character initialization.
func (r *RobotVo) QueueDisjointAfterRun(cost uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AfterRunDisjointCost = cost
}

func (r *RobotVo) runDisjointAfterLoginUnsafe() {
	cost := r.AfterRunDisjointCost
	r.AfterRunDisjointCost = 0
	if cost > 0 {
		go r.OpenDisjointStore(cost)
	}
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
	r.storeDisplayRetryPlans = nil
	r.storeDisplayRetryIndex = 0
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

func formatStoreDisplayItems(items []StoreInfo) string {
	if len(items) == 0 {
		return "-"
	}
	var out strings.Builder
	for index, item := range items {
		if index > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, "%d:%d:%d:%d:%d", item.Index, item.ItemID, item.BoxType, item.BoxIndex, item.Count)
	}
	return out.String()
}

func formatStoreInventory(items map[int]Transaction, limit int) string {
	if len(items) == 0 {
		return "-"
	}
	positions := make([]int, 0, len(items))
	for position := range items {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	if limit > 0 && len(positions) > limit {
		positions = positions[:limit]
	}
	var out strings.Builder
	for index, position := range positions {
		if index > 0 {
			out.WriteByte(',')
		}
		item := items[position]
		fmt.Fprintf(&out, "%d:%d:%d", position, item.ItemId, item.ItemNum)
	}
	if len(positions) < len(items) {
		fmt.Fprintf(&out, ",...+%d", len(items)-len(positions))
	}
	return out.String()
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
	if len(storeInfo) > privateStoreDisplayLimit {
		storeInfo = storeInfo[:privateStoreDisplayLimit]
	}
	if len(r.storeDisplayCandidates) == 0 {
		r.storeDisplayCandidates = append([]StoreInfo(nil), storeInfo...)
		r.storeDisplayRetryPlans = buildPrivateStoreRetryPlans(storeInfo)
		r.storeDisplayRetryIndex = 0
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
	return true
}

// retryPrivateStoreDisplayUnsafe handles CMD 90 error 0x11. Candidates come
// from either CMD 13 or the inventory image committed before the NoCache
// relogin. Preserve the original gradual downgrade so one rejected item does
// not prevent the remaining valid items from opening.
func (r *RobotVo) retryPrivateStoreDisplayUnsafe() bool {
	if r.storeDisplayRetryIndex >= len(r.storeDisplayRetryPlans) {
		return false
	}
	next := append([]StoreInfo(nil), r.storeDisplayRetryPlans[r.storeDisplayRetryIndex]...)
	r.storeDisplayRetryIndex++
	for index := range next {
		next[index].Index = index
	}
	r.StoreDisplaySent = false
	r.StoreDisplayRejected = false
	r.LastStoreError = 0
	return r.completeDisplay(r.PendingStoreTitle, next)
}

func buildPrivateStoreRetryPlans(candidates []StoreInfo) [][]StoreInfo {
	if len(candidates) <= 1 {
		return nil
	}
	plans := make([][]StoreInfo, 0, len(candidates)*2-2)
	for count := len(candidates) - 1; count >= 1; count-- {
		plans = appendRetryPlan(plans, candidates[:count])
	}
	for index := 1; index < len(candidates); index++ {
		plans = appendRetryPlan(plans, []StoreInfo{candidates[index]})
	}
	return plans
}

func appendRetryPlan(plans [][]StoreInfo, plan []StoreInfo) [][]StoreInfo {
	if len(plan) == 0 {
		return plans
	}
	copyPlan := append([]StoreInfo(nil), plan...)
	for index := range copyPlan {
		copyPlan[index].Index = index
	}
	return append(plans, copyPlan)
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
	fmt.Printf("[STORE_INVENTORY_NOT_READY] uid=%d inventory=%d entries=%s\n", r.UID, len(r.InfanMap), formatStoreInventory(r.InfanMap, 24))
}

func (r *RobotVo) GetDbDataAndCompleteDisplay() bool {
	r.mu.Lock()
	if r.State != StateRun || r.partyActiveUnsafe() || !r.StoreCreated || r.DB == nil {
		r.mu.Unlock()
		return false
	}
	uid := r.UID
	db := r.DB
	title := r.PendingStoreTitle
	inventoryVersion := r.storeInventoryVersion
	inventory := make(map[int]Transaction, len(r.InfanMap))
	for position, transaction := range r.InfanMap {
		inventory[position] = transaction
	}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeQueryTimeout)
	defer cancel()
	rows, err := sqlpkg.SelectContext(ctx, db, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID", uid)
	if err != nil {
		return false
	}

	storeInfo := reconcileStoreDisplay(rows, inventory)
	source := "cmd13"
	if len(storeInfo) < min(len(rows), privateStoreDisplayLimit) {
		if preparedInventory, loadErr := loadPreparedStoreInventory(ctx, db, int(uid)); loadErr == nil {
			preparedStoreInfo := reconcileStoreDisplay(rows, preparedInventory)
			if len(preparedStoreInfo) > len(storeInfo) {
				storeInfo = preparedStoreInfo
				source = "prepared_db"
			}
		} else {
			fmt.Printf("[STORE_PREPARED_INVENTORY_ERROR] uid=%d err=%v\n", uid, loadErr)
		}
	}
	wanted := min(len(rows), privateStoreDisplayLimit)
	if len(storeInfo) < wanted {
		fmt.Printf("[STORE_DISPLAY_DEGRADED] uid=%d source=%s rows=%d items=%d entries=%s\n", uid, source, len(rows), len(storeInfo), formatStoreDisplayItems(storeInfo))
	}

	if len(storeInfo) == 0 {
		return false
	}

	customTitle := title != ""
	if title == "" {
		title = "store"
	}
	cfgRows, _ := sqlpkg.SelectContext(ctx, db, "select cfg_content from d_starsky.Robot_stall_config where cfg_type=3 and function_type=2 and state=1 and (UID=? or UID=0) order by UID", uid)
	if len(cfgRows) > 0 && len(cfgRows[0]) > 0 && !customTitle {
		title = cfgRows[0][0]
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateRun || !r.StoreCreated || r.UID != uid || r.DB != db || r.storeInventoryVersion != inventoryVersion {
		return false
	}
	return r.completeDisplay(title, storeInfo)
}

func loadPreparedStoreInventory(ctx context.Context, db *sql.DB, uid int) (map[int]Transaction, error) {
	if db == nil || uid <= 0 {
		return nil, fmt.Errorf("invalid prepared inventory source uid=%d", uid)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT UNCOMPRESS(i.inventory)
		FROM d_starsky.robot_registry r
		JOIN taiwan_cain_2nd.inventory i ON i.charac_no=r.cid
		WHERE r.uid=? LIMIT 1`, uid).Scan(&raw); err != nil {
		return nil, err
	}
	return decodePreparedStoreInventory(raw), nil
}

func decodePreparedStoreInventory(raw []byte) map[int]Transaction {
	const inventorySlotSize = 61
	slotCount := len(raw) / inventorySlotSize
	inventory := make(map[int]Transaction, slotCount)
	for position := 3; position < slotCount; position++ {
		slot := raw[position*inventorySlotSize : (position+1)*inventorySlotSize]
		boxType := int32(binary.BigEndian.Uint16(slot[0:2]))
		itemID := int32(binary.LittleEndian.Uint32(slot[2:6]))
		if boxType <= 0 || itemID <= 0 {
			continue
		}
		inventory[position] = Transaction{
			ItemPos:  int16(position),
			ItemId:   itemID,
			ItemNum:  int32(binary.LittleEndian.Uint32(slot[7:11])),
			ItemType: boxType,
		}
	}
	return inventory
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
			// CMD 13 is the game server's authoritative live inventory. Offline
			// preparation may have requested a larger stack while this session still
			// exposes an older cached quantity. Cap only the quantity, not the item
			// list, so the complete seven-kind display can still be accepted.
			count = available
		}
		if count < 0 {
			continue
		}
		usedSlots[selected.ItemPos] = struct{}{}
		storeInfo = append(storeInfo, StoreInfo{
			Index:  len(storeInfo),
			ItemID: tradeItem,
			// In the tested DFGamer protocol, CMD 90 uses item space 0 with a
			// global inventory index. Sending material item space 7 is rejected
			// with 0x11 even when the referenced material is otherwise valid.
			BoxType:  0,
			BoxIndex: int(selected.ItemPos),
			Price:    price,
			Count:    count,
		})
	}
	return storeInfo
}
