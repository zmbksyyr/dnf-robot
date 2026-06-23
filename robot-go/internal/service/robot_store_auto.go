package service

import (
	"fmt"
	"time"
)

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
	slot, ok := m.acquireAutoStoreSlot(rc)
	if !ok {
		m.autoMu.Lock()
		delete(m.autoStoreBusy, info.UID)
		m.autoMu.Unlock()
		return false
	}
	defer func() {
		m.releaseAutoStoreSlot(slot)
		m.autoMu.Lock()
		delete(m.autoStoreBusy, info.UID)
		m.autoMu.Unlock()
	}()
	points := m.storePoints()
	for try := 1; try <= tries; try++ {
		if shouldStop != nil && shouldStop() {
			robotLogf("[AutoStore] uid=%d cancelled_before_try=%d\n", info.UID, try)
			return false
		}
		pos, ok := points.claim(info.UID)
		if !ok {
			robotLogf("[AutoStore] uid=%d no_store_point try=%d/%d\n", info.UID, try, tries)
			break
		}
		info.Village, info.Area, info.X, info.Y = pos.Village, pos.Area, pos.X, pos.Y
		robotLogf("[AutoStore] uid=%d try=%d/%d source=%s point=%s region=%s pos=%d/%d/%d/%d\n", info.UID, try, tries, pos.Source, pos.PointID, pos.Region, info.Village, info.Area, info.X, info.Y)
		if m.tryAutoStorePosition(info, rc, try, shouldStop) {
			points.report(info.UID, pos, try, true, "store_ack")
			robotLogf("[StoreSuccessPoint] uid=%d point=%s region=%s village=%d area=%d x=%d y=%d try=%d source=%s\n", info.UID, pos.PointID, pos.Region, info.Village, info.Area, info.X, info.Y, try, pos.Source)
			m.addAutoStore(1, 0, 0)
			return true
		}
		points.report(info.UID, pos, try, false, "store_failed")
	}
	points.flush()
	_, _ = m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	_ = m.revokeStorePermission(info.UID, info.CID)
	m.doll.ResetPrivateStore(info.UID)
	robotLogf("[AutoStore] uid=%d failed_after=%d\n", info.UID, tries)
	m.addAutoStore(0, 1, 0)
	m.restoreAutoNormalPosition(info, rc, "store_failed")
	return false
}

func (m *RobotManager) acquireAutoStoreSlot(rc robotRuntimeConfig) (chan struct{}, bool) {
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
		return slots, true
	default:
		robotLogf("[AutoStore] store_concurrent_limit limit=%d\n", limit)
		return nil, false
	}
}

func (m *RobotManager) releaseAutoStoreSlot(slots chan struct{}) {
	if slots == nil {
		return
	}
	select {
	case <-slots:
	default:
	}
}

func (m *RobotManager) tryAutoStorePosition(info RobotInfo, rc robotRuntimeConfig, try int, shouldStop func() bool) bool {
	if shouldStop != nil && shouldStop() {
		return false
	}
	logoutResult, logoutErr := m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	robotLogf("[AutoStore] uid=%d pre_prepare_logout try=%d confirmed=%d failed=%d err=%v\n",
		info.UID, try, logoutResult.Confirmed, logoutResult.Failed, logoutErr)
	logoutDelay := time.Duration(rc.ReconnectDelayMS) * time.Millisecond
	if logoutDelay < 1500*time.Millisecond {
		logoutDelay = 1500 * time.Millisecond
	}
	if sleepWithStop(logoutDelay, shouldStop) {
		return false
	}
	if shouldStop != nil && shouldStop() {
		return false
	}
	if res, err := m.db.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=?,function_type=2 WHERE UID=?", info.Village, info.Area, info.X, info.Y, info.UID); err != nil {
		robotLogf("[AutoStore] uid=%d dummy_update_failed try=%d err=%v\n", info.UID, try, err)
		return false
	} else if affected, err := res.RowsAffected(); err == nil {
		robotLogf("[AutoStore] uid=%d dummy_updated try=%d rows=%d pos=%d/%d/%d/%d\n", info.UID, try, affected, info.Village, info.Area, info.X, info.Y)
	}
	if err := m.syncRobotCharacterVillage(info.CID, info.Village); err != nil {
		robotLogf("[AutoStore] uid=%d charac_village_sync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false
	}
	if err := m.ensureStoreInventoryAndStall(info, rc); err != nil {
		robotLogf("[AutoStore] uid=%d prepare_failed try=%d err=%v\n", info.UID, try, err)
		return false
	}
	robotLogf("[AutoStore] uid=%d prepare_ok try=%d cid=%d pos=%d/%d/%d/%d\n", info.UID, try, info.CID, info.Village, info.Area, info.X, info.Y)
	if sleepWithStop(800*time.Millisecond, shouldStop) {
		return false
	}
	online, err := m.online(RobotCommandRequest{UIDs: []int{info.UID}}, false, true)
	robotLogf("[AutoStore] uid=%d online_result try=%d confirmed=%d failed=%d err=%v\n", info.UID, try, online.Confirmed, online.Failed, err)
	if err != nil || online.Confirmed != 1 {
		robotLogf("[AutoStore] uid=%d store_online_failed try=%d confirmed=%d failed=%d err=%v\n", info.UID, try, online.Confirmed, online.Failed, err)
		m.doll.ResetPrivateStore(info.UID)
		return false
	}
	if err := m.syncRobotCharacterVillage(info.CID, info.Village); err != nil {
		robotLogf("[AutoStore] uid=%d charac_village_resync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false
	}
	if shouldStop != nil && shouldStop() {
		return false
	}
	fromGate := storeGateAreaForVillage(info.Village)
	if fromGate != info.Area {
		areaSet := m.doll.SetAreaFrom(info.UID, info.Village, info.Area, info.X, info.Y, info.Village, fromGate)
		robotLogf("[AutoStore] uid=%d set_area_from_gate try=%d ok=%t from=%d/%d target=%d/%d/%d/%d\n",
			info.UID, try, areaSet, info.Village, fromGate, info.Village, info.Area, info.X, info.Y)
		if !areaSet {
			m.doll.ResetPrivateStore(info.UID)
			return false
		}
		if sleepWithStop(1800*time.Millisecond, shouldStop) {
			return false
		}
	}
	if st, ok := m.runtimeStatusMap()[info.UID]; ok {
		robotLogf("[AutoStore] uid=%d before_store try=%d state=%s disconnect=%d runtime_pos=%d/%d/%d/%d target_pos=%d/%d/%d/%d\n",
			info.UID, try, st.StateName, st.DisconnectReason, st.Village, st.Area, st.X, st.Y, info.Village, info.Area, info.X, info.Y)
	}
	title := fmt.Sprintf("tw-%d", info.UID%100000)
	if !m.doll.StartPrivateStore(info.UID, title) {
		robotLogf("[AutoStore] uid=%d store_start_failed try=%d\n", info.UID, try)
		m.doll.ResetPrivateStore(info.UID)
		return false
	}
	if m.autoWaitStoreDisplay(info.UID, rc, shouldStop) {
		return true
	}
	_, _ = m.Logout(RobotCommandRequest{UIDs: []int{info.UID}})
	m.doll.ResetPrivateStore(info.UID)
	return false
}

func (m *RobotManager) autoWaitStoreDisplay(uid int, rc robotRuntimeConfig, shouldStop func() bool) bool {
	started := time.Now()
	var createdAt time.Time
	var lastDisplayAt time.Time
	displayTries := 0
	deadline := time.Now().Add(10 * time.Second)
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
		if st.StoreDisplayRejected {
			robotLogf("[AutoStore] uid=%d display_rejected state=%s disconnect=%d robot_type=%d store_created=%t wait_ms=%d tries=%d\n",
				uid, st.StateName, st.DisconnectReason, st.RobotType, st.StoreCreated, time.Since(started).Milliseconds(), displayTries)
			return false
		}
		if st.StoreCreateRejected && !st.StoreCreated {
			robotLogf("[AutoStore] uid=%d create_rejected state=%s disconnect=%d robot_type=%d wait_ms=%d tries=%d\n",
				uid, st.StateName, st.DisconnectReason, st.RobotType, time.Since(started).Milliseconds(), displayTries)
			return false
		}
		if st.StoreCreated && createdAt.IsZero() {
			createdAt = time.Now()
		}
		if !createdAt.IsZero() && time.Since(createdAt) >= 2*time.Second &&
			(lastDisplayAt.IsZero() || time.Since(lastDisplayAt) >= 2*time.Second) && displayTries < 4 {
			lastDisplayAt = time.Now()
			displayTries++
			if m.doll.CompletePrivateStoreDisplay(uid) {
				return true
			}
		}
		if sleepWithStop(200*time.Millisecond, shouldStop) {
			return false
		}
	}
	if st, ok := m.runtimeStatusMap()[uid]; ok {
		robotLogf("[AutoStore] uid=%d display_wait_failed state=%s disconnect=%d robot_type=%d store_created=%t display_sent=%t display_ack=%t wait_ms=%d tries=%d\n",
			uid, st.StateName, st.DisconnectReason, st.RobotType, st.StoreCreated, st.StoreDisplaySent, st.StoreDisplayAck, time.Since(started).Milliseconds(), displayTries)
	}
	return false
}

func (m *RobotManager) restoreAutoNormalPosition(info RobotInfo, rc robotRuntimeConfig, reason string) RobotInfo {
	maps := m.loadMapCatalog()
	normal := m.randomNormalPosition(info, rc, maps)
	_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=?,function_type=0 WHERE UID=?",
		normal.Village, normal.Area, normal.X, normal.Y, normal.UID)
	if err := m.syncRobotCharacterVillage(normal.CID, normal.Village); err != nil {
		robotLogf("[AutoStore] uid=%d restore_charac_village_sync_failed cid=%d village=%d err=%v\n",
			normal.UID, normal.CID, normal.Village, err)
	}
	robotLogf("[AutoStore] uid=%d restore_normal reason=%s pos=%d/%d/%d/%d\n",
		normal.UID, reason, normal.Village, normal.Area, normal.X, normal.Y)
	return normal
}

func (m *RobotManager) syncRobotCharacterVillage(cid int, village int) error {
	if cid <= 0 {
		return fmt.Errorf("invalid cid %d", cid)
	}
	if _, err := m.db.Exec("UPDATE taiwan_cain.charac_info SET village=? WHERE charac_no=?", village, cid); err != nil {
		return fmt.Errorf("update charac_info: %w", err)
	}
	if _, err := m.db.Exec("UPDATE taiwan_cain.charac_stat SET village=?,village_prev=? WHERE charac_no=?", village, village, cid); err != nil {
		return fmt.Errorf("update charac_stat: %w", err)
	}
	var infoVillage, statVillage, statPrev int
	if err := m.db.QueryRow(`SELECT ci.village,cs.village,cs.village_prev
		FROM taiwan_cain.charac_info ci JOIN taiwan_cain.charac_stat cs ON cs.charac_no=ci.charac_no
		WHERE ci.charac_no=?`, cid).Scan(&infoVillage, &statVillage, &statPrev); err != nil {
		return fmt.Errorf("verify charac village: %w", err)
	}
	if infoVillage != village || statVillage != village || statPrev != village {
		return fmt.Errorf("verify charac village mismatch want=%d info=%d stat=%d prev=%d", village, infoVillage, statVillage, statPrev)
	}
	robotLogf("[AutoStore] cid=%d charac_village_synced village=%d stat_prev=%d\n", cid, statVillage, statPrev)
	return nil
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
