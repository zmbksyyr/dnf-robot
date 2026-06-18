package service

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (m *RobotManager) Store(req RobotCommandRequest) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	rc := m.loadRobotConfig()
	status := m.runtimeStatusMap()
	result := newCommandResult(len(robots))
	var offline []RobotInfo
	for _, r := range robots {
		if err := m.ensureStoreInventoryAndStall(r, rc); err != nil {
			return RobotCommandResult{}, err
		}
		if st, ok := status[r.UID]; ok && activeRuntimeStatus(st) {
			logoutResult, err := m.Logout(RobotCommandRequest{UIDs: []int{r.UID}})
			if err != nil || logoutResult.Confirmed == 0 {
				msg := fmt.Sprintf("logout before store failed: err=%v confirmed=%d", err, logoutResult.Confirmed)
				robotLogf("[Store] uid=%d %s\n", r.UID, msg)
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "logout_failed", Message: msg})
				continue
			}
			if rc.ReconnectDelayMS > 0 {
				time.Sleep(time.Duration(rc.ReconnectDelayMS) * time.Millisecond)
			}
		}
		offline = append(offline, r)
	}
	if len(offline) > 0 {
		online, err := m.Online(RobotCommandRequest{UIDs: robotUIDs(offline)}, false)
		if err != nil {
			return result, err
		}
		onlineOK := make(map[int]RobotActionResult)
		for _, robot := range online.Robots {
			if robot.OK && robot.State == "running" {
				onlineOK[robot.UID] = robot
			}
		}
		for _, r := range offline {
			if _, ok := onlineOK[r.UID]; !ok {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "not_online", Message: "online before store failed"})
				continue
			}
			title := fmt.Sprintf("tw-%d", r.UID%100000)
			if m.doll.StartPrivateStore(r.UID, title) {
				_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET function_type=2 WHERE UID=?", r.UID)
				result.Accepted++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
			} else {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "store_start_failed", Message: "StartPrivateStore failed after online"})
			}
		}
	}
	deadline := time.Now().Add(time.Duration(rc.StoreConfirmTimeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		status = m.runtimeStatusMap()
		for i := range result.Robots {
			if result.Robots[i].OK || result.Robots[i].State != "accepted" {
				continue
			}
			st, ok := status[result.Robots[i].UID]
			if !ok || !st.StoreDisplayAck {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	status = m.runtimeStatusMap()
	for i := range result.Robots {
		if result.Robots[i].OK || result.Robots[i].State != "accepted" {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) && st.StoreDisplayAck {
			result.Robots[i].OK = true
			result.Robots[i].State = "store"
			result.Confirmed++
		} else {
			result.Robots[i].State = "not_confirmed"
			result.Robots[i].Message = "store state not confirmed"
			result.Failed++
			_ = m.revokeStorePermission(result.Robots[i].UID, result.Robots[i].CID)
			m.doll.ResetPrivateStore(result.Robots[i].UID)
		}
	}
	return result, nil
}

func (m *RobotManager) autoStoreUntilSuccess(st RuntimeRobotStatus, rc robotRuntimeConfig, shouldStop func() bool) bool {
	tries := rc.AutoStoreMaxPositionTries
	if tries <= 0 {
		tries = 10
	}
	info := RobotInfo{UID: st.UID, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y, Port: m.cfg.RobotGamePort}
	if robots, err := m.selectRobots(RobotCommandRequest{UIDs: []int{st.UID}}); err == nil && len(robots) > 0 {
		info.CID = robots[0].CID
		info.Port = robots[0].Port
		info.Level = robots[0].Level
		info.Job = robots[0].Job
		info.Grow = robots[0].Grow
	}
	m.autoMu.Lock()
	if m.autoStoreBusy[info.UID] {
		m.autoMu.Unlock()
		return false
	}
	m.autoStoreBusy[info.UID] = true
	m.autoMu.Unlock()
	if !m.acquireAutoStoreSlot(rc) {
		m.autoMu.Lock()
		delete(m.autoStoreBusy, info.UID)
		m.autoMu.Unlock()
		return false
	}
	defer func() {
		m.releaseAutoStoreSlot()
		m.autoMu.Lock()
		delete(m.autoStoreBusy, info.UID)
		m.autoMu.Unlock()
	}()
	positions := m.storeAttemptPositions(info, rc, tries)
	for try, pos := range positions {
		if shouldStop != nil && shouldStop() {
			robotLogf("[AutoStore] uid=%d cancelled_before_try=%d\n", info.UID, try+1)
			return false
		}
		info.Village, info.Area, info.X, info.Y = pos.Village, pos.Area, pos.X, pos.Y
		robotLogf("[AutoStore] uid=%d try=%d/%d source=%s pos=%d/%d/%d/%d\n", info.UID, try+1, len(positions), pos.Source, info.Village, info.Area, info.X, info.Y)
		if m.tryAutoStorePosition(info, rc, try+1, shouldStop) {
			m.recordStoreSuccessPoint(info, try+1, pos.Source)
			m.addAutoStore(1, 0, 0)
			return true
		}
	}
	_, _ = m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	_ = m.revokeStorePermission(info.UID, info.CID)
	m.doll.ResetPrivateStore(info.UID)
	robotLogf("[AutoStore] uid=%d failed_after=%d\n", info.UID, tries)
	m.addAutoStore(0, 1, 0)
	m.restoreAutoNormalPosition(info, rc, "store_failed")
	return false
}

func (m *RobotManager) acquireAutoStoreSlot(rc robotRuntimeConfig) bool {
	limit := rc.SchedulerStoreConcurrent
	if limit <= 0 {
		limit = 30
	}
	m.storeSlotMu.Lock()
	if m.autoStoreSlots == nil || m.autoStoreCap != limit {
		m.autoStoreSlots = make(chan struct{}, limit)
		m.autoStoreCap = limit
	}
	slots := m.autoStoreSlots
	m.storeSlotMu.Unlock()
	select {
	case slots <- struct{}{}:
		return true
	default:
		robotLogf("[AutoStore] store_concurrent_limit limit=%d\n", limit)
		return false
	}
}

func (m *RobotManager) releaseAutoStoreSlot() {
	m.storeSlotMu.Lock()
	slots := m.autoStoreSlots
	m.storeSlotMu.Unlock()
	if slots == nil {
		return
	}
	select {
	case <-slots:
	default:
	}
}

func (m *RobotManager) storeAttemptPositions(info RobotInfo, rc robotRuntimeConfig, maxTries int) []storePosition {
	if maxTries <= 0 {
		maxTries = 10
	}
	out := make([]storePosition, 0, maxTries)
	success := m.storeSuccessPositions(info.Village, info.Area, minInt(4, maxTries))
	out = append(out, success...)
	for i := len(out); i < maxTries; i++ {
		village, area, x, y := m.randomStorePositionNear(info, rc)
		out = append(out, storePosition{Village: village, Area: area, X: x, Y: y, Source: "random"})
	}
	return out
}

func (m *RobotManager) tryAutoStorePosition(info RobotInfo, rc robotRuntimeConfig, try int, shouldStop func() bool) bool {
	if shouldStop != nil && shouldStop() {
		return false
	}
	_, _ = m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	if rc.ReconnectDelayMS > 0 {
		if sleepWithStop(time.Duration(rc.ReconnectDelayMS)*time.Millisecond, shouldStop) {
			return false
		}
	}
	if shouldStop != nil && shouldStop() {
		return false
	}
	_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=?,function_type=2 WHERE UID=?", info.Village, info.Area, info.X, info.Y, info.UID)
	if err := m.ensureStoreInventoryAndStall(info, rc); err != nil {
		robotLogf("[AutoStore] uid=%d prepare_failed try=%d err=%v\n", info.UID, try, err)
		return false
	}
	online, err := m.Online(RobotCommandRequest{UIDs: []int{info.UID}}, true)
	if err != nil || online.Confirmed != 1 {
		robotLogf("[AutoStore] uid=%d store_online_failed try=%d confirmed=%d failed=%d err=%v\n", info.UID, try, online.Confirmed, online.Failed, err)
		m.doll.ResetPrivateStore(info.UID)
		return false
	}
	if shouldStop != nil && shouldStop() {
		return false
	}
	if m.autoWaitStoreDisplay(info.UID, rc, shouldStop) {
		return true
	}
	_, _ = m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	m.doll.ResetPrivateStore(info.UID)
	return false
}

func (m *RobotManager) recordStoreSuccessPoint(info RobotInfo, try int, source string) {
	_, _ = m.db.Exec(`INSERT INTO d_starsky.robot_store_success_point (village,area,x,y,uid,success_count,updated_at)
VALUES (?,?,?,?,?,1,NOW())
ON DUPLICATE KEY UPDATE uid=VALUES(uid),success_count=success_count+1,updated_at=NOW()`,
		info.Village, info.Area, info.X, info.Y, info.UID)
	robotLogf("[StoreSuccessPoint] uid=%d village=%d area=%d x=%d y=%d try=%d source=%s\n", info.UID, info.Village, info.Area, info.X, info.Y, try, source)
}

func (m *RobotManager) storeSuccessPositions(village, area, limit int) []storePosition {
	if limit <= 0 {
		limit = 4
	}
	rows, err := m.db.Query(`SELECT village,area,x,y FROM d_starsky.robot_store_success_point
WHERE village=? AND area=?
ORDER BY success_count DESC, updated_at DESC
LIMIT ?`, village, area, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []storePosition
	for rows.Next() {
		var pos storePosition
		if rows.Scan(&pos.Village, &pos.Area, &pos.X, &pos.Y) == nil {
			pos.Source = "success"
			out = append(out, pos)
		}
	}
	return out
}

func (m *RobotManager) autoWaitStoreDisplay(uid int, rc robotRuntimeConfig, shouldStop func() bool) bool {
	displaySent := false
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return false
		}
		st, ok := m.runtimeStatusMap()[uid]
		if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
			return false
		}
		if st.StoreDisplayAck {
			return true
		}
		if st.StoreCreated && !displaySent {
			displaySent = true
			if m.doll.CompletePrivateStoreDisplay(uid) {
				return true
			}
			deadline = time.Now().Add(500 * time.Millisecond)
		}
		if sleepWithStop(200*time.Millisecond, shouldStop) {
			return false
		}
	}
	return false
}

func sleepWithStop(d time.Duration, shouldStop func() bool) bool {
	if d <= 0 {
		return shouldStop != nil && shouldStop()
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return true
		}
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		time.Sleep(remaining)
	}
	return shouldStop != nil && shouldStop()
}

func (m *RobotManager) restoreAutoNormalPosition(info RobotInfo, rc robotRuntimeConfig, reason string) RobotInfo {
	maps := m.loadMapCatalog()
	normal := m.randomNormalPosition(info, rc, maps)
	_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=?,function_type=0 WHERE UID=?",
		normal.Village, normal.Area, normal.X, normal.Y, normal.UID)
	robotLogf("[AutoStore] uid=%d restore_normal reason=%s pos=%d/%d/%d/%d\n",
		normal.UID, reason, normal.Village, normal.Area, normal.X, normal.Y)
	return normal
}

func (m *RobotManager) randomNormalPosition(info RobotInfo, rc robotRuntimeConfig, maps []mapCatalogItem) RobotInfo {
	normal := info
	normal.Village = rc.SpawnFallbackVillage
	normal.Area = rc.SpawnArea
	normal.X = m.randBetween(rc.SpawnXMin, rc.SpawnXMax)
	normal.Y = m.randBetween(rc.SpawnYMin, rc.SpawnYMax)
	if mp, ok := m.randomMap(maps, normal.Level); ok {
		normal.Village = mp.Village
		normal.Area = mp.Area
		normal.X = m.randBetween(mp.XMin, mp.XMax)
		normal.Y = m.randBetween(mp.YMin, mp.YMax)
	}
	m.applyConfiguredLocation(&normal, rc, maps)
	return normal
}

func (m *RobotManager) randomStorePositionNear(info RobotInfo, rc robotRuntimeConfig) (int, int, int, int) {
	xMin, xMax := maxInt(0, info.X-260), info.X+260
	yMin, yMax := maxInt(0, info.Y-70), info.Y+70
	if info.Village == 3 && info.Area == 0 {
		type storeArea struct {
			xMin int
			xMax int
			yMin int
			yMax int
		}
		areas := []storeArea{
			{xMin: 250, xMax: 600, yMin: 180, yMax: 260},
			{xMin: 800, xMax: 1150, yMin: 220, yMax: 270},
			{xMin: 1300, xMax: 1600, yMin: 240, yMax: 320},
			{xMin: 320, xMax: 520, yMin: 390, yMax: 450},
		}
		a := areas[m.randIntn(len(areas))]
		return info.Village, info.Area, m.randBetween(a.xMin, a.xMax), m.randBetween(a.yMin, a.yMax)
	}
	if xMax <= xMin {
		xMin, xMax = rc.SpawnXMin, rc.SpawnXMax
	}
	if yMax <= yMin {
		yMin, yMax = rc.SpawnYMin, rc.SpawnYMax
	}
	return info.Village, info.Area, m.randBetween(xMin, xMax), m.randBetween(yMin, yMax)
}

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

func (m *RobotManager) ensureStorePermission(uid, cid int) error {
	if uid <= 0 || cid <= 0 {
		return nil
	}
	steps := []struct {
		query string
		args  []interface{}
	}{
		{"DELETE FROM taiwan_login.member_premium WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_login.member_premium(pre_type,m_id,service_start,service_end,event_id,server_id) VALUES(8,?,NOW(),'2030-12-31 00:00:00',50002,0)", []interface{}{uid}},
		{"DELETE FROM d_taiwan.member_miles WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO d_taiwan.member_miles(m_id,miles) VALUES(?,7)", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.prod_buy_user WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_prod.prod_buy_user(m_id,user_id,sex,birthday,first_buy_time,last_buy_time) VALUES(?,?,0,'',NOW(),NOW())", []interface{}{uid, strconv.Itoa(uid)}},
		{"DELETE FROM taiwan_prod.pu_user_list WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_prod.pu_user_list(m_id) VALUES(?)", []interface{}{uid}},
		{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=? AND charac_no=?", []interface{}{uid, cid}},
		{"INSERT INTO taiwan_login.dnf_event_entry(event_id,m_id,occ_date,server_id,charac_no,obtain_date) VALUES(50002,?,NOW(),0,?,NOW())", []interface{}{uid, cid}},
	}
	for _, step := range steps {
		if _, err := m.db.Exec(step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func (m *RobotManager) revokeStorePermission(uid, cid int) error {
	if uid <= 0 {
		return nil
	}
	steps := []struct {
		query string
		args  []interface{}
	}{
		{"DELETE FROM taiwan_login.member_premium WHERE pre_type=8 AND m_id=?", []interface{}{uid}},
		{"DELETE FROM d_taiwan.member_miles WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.prod_buy_user WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.pu_user_list WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM d_starsky.Robot_stall WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"DELETE FROM d_starsky.Robot_stall_config WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"UPDATE d_starsky.Dummylist SET function_type=0 WHERE UID=?", []interface{}{uid}},
	}
	if cid > 0 {
		steps = append(steps, struct {
			query string
			args  []interface{}
		}{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=? AND charac_no=?", []interface{}{uid, cid}})
	} else {
		steps = append(steps, struct {
			query string
			args  []interface{}
		}{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=?", []interface{}{uid}})
	}
	for _, step := range steps {
		if _, err := m.db.Exec(step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}
