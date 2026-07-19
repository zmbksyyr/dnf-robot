package dnf

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	sqlpkg "robot/internal/foundation/sql"
)

type AsyncTaskType int

const (
	AsyncMove     AsyncTaskType = 0
	AsyncDisjoint AsyncTaskType = 1
	AsyncPriStore AsyncTaskType = 2
)

type StoreInfo struct {
	Index    int
	BoxType  int
	BoxIndex int
	Price    int
	Count    int
}

type AsyncTask struct {
	Type      AsyncTaskType
	Village   int
	Area      int
	X         int
	Y         int
	Mtype     int
	Speed     int
	Cost      int
	Title     string
	StoreInfo []StoreInfo
}

type Transaction struct {
	ItemPos  int16
	ItemId   int32
	ItemNum  int32
	ItemType int32
}

type ShopVo struct {
	TradeItem  int
	Price      int
	ItemNumber int
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
	r.PrepareStoreAfterItemList = false
	r.PendingStoreTitle = ""
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
	r.PrepareStoreAfterItemList = false
	r.RobotTyp = 2
}

func (r *RobotVo) runAsyncTasks() {
	if r.AfterRunAsyncTaskVec == nil {
		return
	}
	tasks := r.AfterRunAsyncTaskVec
	r.AfterRunAsyncTaskVec = nil
	cost := uint32(0)
	title := ""
	for _, task := range tasks {
		switch task.Type {
		case AsyncDisjoint:
			cost = uint32(task.Cost)
		case AsyncPriStore:
			title = task.Title
		}
	}

	go r.executeAsyncTasks(tasks, cost, title)
}

func (r *RobotVo) executeAsyncTasks(tasks []AsyncTask, cost uint32, title string) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[RobotVo] runAsyncTasks panic uid=%d err=%v\n", r.UID, rec)
		}
	}()
	for _, task := range tasks {
		switch task.Type {
		case AsyncDisjoint:
			r.OpenDisjointStore(cost)
		case AsyncPriStore:
			r.mu.Lock()
			r.PendingStoreTitle = title
			r.mu.Unlock()
			r.CreatePrivateStore()
			r.GetCompleteDisplay(0)
		}
	}
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

func (r *RobotVo) CreatePrivateStore() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
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
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
	r.RobotTyp = 2
}

func (r *RobotVo) CompleteDisplay(title string, storeInfo []StoreInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeDisplay(title, storeInfo)
}

func (r *RobotVo) completeDisplay(title string, storeInfo []StoreInfo) {
	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	if r.StoreDisplaySent {
		return
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
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
		r.StoreDisplaySent = true
		r.StoreDisplayAck = false
	}
}

func (r *RobotVo) getShopVo(functionType int) map[int]ShopVo {
	result := make(map[int]ShopVo)
	if r.DB == nil {
		return result
	}

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=? and state=1 and (UID=? or UID=0) order by UID", functionType, r.UID)
	if err != nil {
		fmt.Printf("getShopVo query error: %v\n", err)
		return result
	}

	for _, row := range rows {
		if len(row) >= 3 && row[0] != "" && row[1] != "" && row[2] != "" {
			tradeItem, _ := strconv.Atoi(row[0])
			price, _ := strconv.Atoi(row[1])
			itemNumber, _ := strconv.Atoi(row[2])
			if price > 0 {
				result[tradeItem] = ShopVo{TradeItem: tradeItem, Price: price, ItemNumber: itemNumber}
			}
		}
	}
	return result
}

func (r *RobotVo) GetCompleteDisplay(flag int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}
	if !r.StoreCreated {
		return false
	}

	r.IsWaitingItemList = true
	var data [8]byte
	data[0] = byte(flag)
	pkt, err := buildSendPacket(20, uint16(r.PacketID), data[:], r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
	return err == nil
}

func (r *RobotVo) GetDbDataAndCompleteDisplay() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.partyActiveUnsafe() {
		return false
	}
	if !r.StoreCreated {
		return false
	}
	return r.getDbDataAndCompleteDisplayUnsafe()
}

func (r *RobotVo) getDbDataAndCompleteDisplayUnsafe() bool {
	if r.State != StateRun {
		return false
	}

	if len(r.InfanMap) == 0 || r.DB == nil {
		return false
	}

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID", r.UID)
	if err != nil {
		return false
	}

	var storeInfo []StoreInfo
	for _, row := range rows {
		if len(row) >= 3 && row[0] != "" && row[1] != "" && row[2] != "" {
			tradeItem, _ := strconv.Atoi(row[0])
			price, _ := strconv.Atoi(row[1])
			itemNumber, _ := strconv.Atoi(row[2])
			if price > 0 {
				if tx, ok := r.InfanMap[tradeItem]; ok {
					storeInfo = append(storeInfo, StoreInfo{
						Index:    len(storeInfo),
						BoxIndex: int(tx.ItemPos),
						Price:    price,
						Count:    itemNumber,
					})
				}
			}
		}
	}

	if len(storeInfo) > 0 {
		title := r.PendingStoreTitle
		if title == "" {
			title = "store"
		}

		cfgRows, _ := sqlpkg.Select(r.DB, "select cfg_content from d_starsky.Robot_stall_config where cfg_type=3 and function_type=2 and state=1 and (UID=? or UID=0) order by UID", r.UID)
		if len(cfgRows) > 0 && len(cfgRows[0]) > 0 && r.PendingStoreTitle == "" {
			title = cfgRows[0][0]
		}

		r.completeDisplay(title, storeInfo)
	}

	return true
}

func (r *RobotVo) CompleteDisplayFromStallFallback() bool {
	r.mu.Lock()
	if r.State != StateRun || !r.StoreCreated || r.StoreDisplayAck || r.DB == nil {
		r.mu.Unlock()
		return false
	}
	uid := r.UID
	title := r.PendingStoreTitle
	r.mu.Unlock()

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID,id limit 24", uid)
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
	err = r.DB.QueryRow("SELECT UNCOMPRESS(i.inventory) FROM taiwan_cain.charac_info c JOIN taiwan_cain_2nd.inventory i ON i.charac_no=c.charac_no WHERE c.m_id=? ORDER BY c.charac_no LIMIT 1", uid).Scan(&invRaw)
	if err == nil {
		for rawIndex := 0; rawIndex+1 < 249 && (rawIndex+1)*61 <= len(invRaw); rawIndex++ {
			slot := invRaw[rawIndex*61 : (rawIndex+1)*61]
			boxType := int(binary.BigEndian.Uint16(slot[0:2]))
			itemID := int(binary.LittleEndian.Uint32(slot[2:6]))
			count := int(binary.LittleEndian.Uint32(slot[7:11]))
			if boxType > 0 && itemID > 0 {
				gameIndex := rawIndex - 2
				if gameIndex <= 0 {
					gameIndex = rawIndex
				}
				pos := invPos{BoxType: boxType, RawBoxIndex: rawIndex, GameBoxIndex: gameIndex, Count: count}
				if old, ok := inventory[itemID]; !ok || (!isStoreStackableType(old.BoxType) && isStoreStackableType(boxType)) {
					inventory[itemID] = pos
				}
			}
		}
	}

	type storeRow struct {
		Price int
		Count int
		Pos   invPos
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
		if !isStoreStackableType(pos.BoxType) {
			continue
		}
		if pos.Count > 0 && itemNumber > pos.Count {
			itemNumber = pos.Count
		}
		storeRows = append(storeRows, storeRow{Price: price, Count: itemNumber, Pos: pos})
	}
	if len(storeRows) == 0 {
		return false
	}
	if title == "" {
		title = "store"
	}

	r.mu.Lock()
	if r.State != StateRun || !r.StoreCreated || r.StoreDisplayAck || r.UID != uid || r.DB == nil {
		r.mu.Unlock()
		return r.StoreDisplayAck
	}
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.mu.Unlock()

	storeInfo := make([]StoreInfo, 0, len(storeRows))
	for i, sr := range storeRows {
		count := sr.Count
		if count <= 0 {
			count = 1
		}
		storeInfo = append(storeInfo, StoreInfo{Index: i, BoxType: 0, BoxIndex: sr.Pos.GameBoxIndex, Price: sr.Price, Count: count})
	}

	r.CompleteDisplay(title, storeInfo)
	time.Sleep(1400 * time.Millisecond)
	return r.Snapshot().StoreDisplayAck
}

func isStoreStackableType(itemType int) bool {
	switch itemType {
	case 2, 3, 4, 10:
		return true
	default:
		return false
	}
}
