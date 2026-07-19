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

func (r *RobotVo) SendMsg(buf []byte) bool {
	return r.sendRaw(buf)
}

func (r *RobotVo) SendPublicMessage(msgType int, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	msgStr := string(msg)
	sendMsgType := byte(msgType)
	if sendMsgType == 0 {
		sendMsgType = 0x03
	}
	if basePos := findSubstring(msgStr, "ext("); basePos >= 0 {
		fPos := basePos + 4
		if endP := findSubstring(msgStr[fPos:], ")"); endP >= 0 {
			if t, err := strconv.Atoi(msgStr[fPos : fPos+endP]); err == nil {
				sendMsgType = byte(t)
			}
			msg = msg[:basePos]
		}
	}

	r.sendPublicMessagePacket(sendMsgType, 0x24, msg)
}

func (r *RobotVo) sendPublicMessagePacket(msgType, flag byte, msg []byte) {
	realSize := 1 + 2 + 4 + 4 + len(msg)
	alinSize := alignTo16(realSize)
	data := make([]byte, alinSize)
	data[0] = msgType
	data[1] = flag
	binary.LittleEndian.PutUint32(data[7:11], uint32(len(msg)))
	copy(data[11:], msg)
	pkt, err := buildSendPacket(17, uint16(r.PacketID), data, r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
}

func (r *RobotVo) SetArea(village, area uint8, x, y uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, uint16(r.CurVillage), uint16(r.CurArea))
}

func (r *RobotVo) SetAreaFrom(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, fromVillage, fromArea)
}

func (r *RobotVo) setAreaFromLocked(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	areaChanged := r.CurVillage != village || r.CurArea != area
	setArea := r.setArea
	setArea[0] = village
	setArea[1] = area
	binary.LittleEndian.PutUint16(setArea[2:4], x)
	binary.LittleEndian.PutUint16(setArea[4:6], y)
	setArea[7] = 0x01
	binary.LittleEndian.PutUint16(setArea[8:10], fromVillage)
	binary.LittleEndian.PutUint16(setArea[10:12], fromArea)

	pkt, err := buildSendPacket(38, uint16(r.PacketID), setArea[:], r.Cipher)
	r.PacketID++
	if err == nil {
		if r.SendMsg(pkt) {
			if areaChanged {
				r.townEntityPositions = make(map[uint16]townEntityPosition)
			}
			r.CurVillage = village
			r.CurArea = area
			r.CurX = x
			r.CurY = y
		}
	}
}

func (r *RobotVo) SetPosition(x, y uint16, typ uint8, speed uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	r.setPositionUnsafe(x, y, typ, speed)
}

func (r *RobotVo) setPositionUnsafe(x, y uint16, typ uint8, speed uint16) bool {
	var setPos [8]byte
	setPos[0] = 0xDA
	setPos[1] = 0x01
	setPos[2] = 0xEA
	setPos[4] = 0x05
	setPos[5] = 0x64

	binary.LittleEndian.PutUint16(setPos[0:2], x)
	binary.LittleEndian.PutUint16(setPos[2:4], y)
	setPos[4] = typ
	binary.LittleEndian.PutUint16(setPos[5:7], speed)

	pkt, err := buildSendPacket(37, uint16(r.PacketID), setPos[:], r.Cipher)
	r.PacketID++
	if err != nil || !r.SendMsg(pkt) {
		return false
	}
	r.CurX = x
	r.CurY = y
	r.MoveType = typ
	return true
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

func (r *RobotVo) CheckUserState() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		return false
	}
	if r.DisconReason != NoDisconnect {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		return false
	}
	if r.State != StateRun {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		return false
	}
	partyActive := r.partyActiveUnsafe()
	if partyActive {
		r.ensurePartyUDPLoopUnsafe()
		r.ensurePartyRelayUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
	} else if r.partyRelayConn != nil {
		r.closePartyRelayUnsafe()
	}
	return true
}

func (r *RobotVo) RefishConnect() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		if r.Controller != nil {
			r.Controller.Delete(r.UID)
		}
		return false
	}

	if r.State == StateRun || r.State == StateLogin || r.State == StateInit {
		r.recvBuffer = nil
		r.recvSize = 0

		newVo := NewRobotVo(r.DB)
		newVo.Controller = r.Controller
		newVo.Load(r.LoginInfo)
		newVo.RunStartTime = r.RunStartTime
		newVo.ConnCount = r.ConnCount + 1
		newVo.AfterRunAsyncTaskVec = r.AfterRunAsyncTaskVec
		newVo.PendingStoreTitle = r.PendingStoreTitle

		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop

		if newVo.ConnCount < newVo.MaxReConn {
			if r.Controller != nil {
				r.Controller.Delete(r.UID)
			}
			delaySec := int(newVo.ReDelay)
			if delaySec <= 0 {
				delaySec = 5
			} else {
				delaySec = int((time.Duration(newVo.ReDelay)*time.Millisecond + time.Second - 1) / time.Second)
			}
			if r.Controller != nil {
				r.Controller.AddMessageDelay("MsgOnLine", newVo, delaySec)
			}
		}
		return true
	}

	r.State = StateStop
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	return true
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

func dnfGateAreaForVillage(village int) int {
	if area, ok := dnfGateAreaByVillage[village]; ok {
		return area
	}
	return 1
}

var dnfGateAreaByVillage = map[int]int{
	1: 1, 2: 5, 3: 2, 4: 1, 5: 1, 6: 4, 8: 1, 9: 2, 10: 1, 11: 3,
	14: 3, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 7, 23: 0,
	24: 0, 25: 0, 26: 0,
}

func isStoreStackableType(itemType int) bool {
	switch itemType {
	case 2, 3, 4, 10:
		return true
	default:
		return false
	}
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
