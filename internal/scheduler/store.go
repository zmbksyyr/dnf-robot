package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	storecap "robot/internal/capability/store"
	"robot/internal/foundation/mathx"
)

func (m *RobotManager) storePoints() *storecap.PointCoordinator {
	var points *storecap.PointCoordinator
	_ = m.lockHub().WithResource(lockScopeScheduler, lockResourceSchedulerStorePoints, "store_points", func() error {
		if m.storePointsCoord == nil {
			configDir := ""
			if m.cfg != nil {
				configDir = m.cfg.ConfigDir
			}
			m.storePointsCoord = storecap.NewPointCoordinator(configDir, robotLogf)
		}
		points = m.storePointsCoord
		return nil
	})
	return points
}

func (m *RobotManager) acquireAutoStoreSlot(rc robotconfig.RuntimeConfig) (chan struct{}, bool) {
	limit := normalizedStoreConcurrent(rc)
	var slots chan struct{}
	_ = m.lockHub().WithResource(lockScopeScheduler, lockResourceSchedulerStoreSlots, "acquire_auto_store_slot", func() error {
		if m.autoStoreSlots == nil || m.autoStoreCap != limit {
			m.autoStoreSlots = make(chan struct{}, limit)
			m.autoStoreCap = limit
		}
		slots = m.autoStoreSlots
		return nil
	})
	select {
	case slots <- struct{}{}:
		return slots, true
	default:
		return nil, false
	}
}

func normalizedStoreConcurrent(rc robotconfig.RuntimeConfig) int {
	limit := rc.SchedulerStoreConcurrent
	if limit <= 0 {
		limit = 30
	}
	return limit
}

func (m *RobotManager) acquireAutoItemStoreSlot(rc robotconfig.RuntimeConfig) (func(), bool) {
	itemLimit := m.effectiveAutoItemStoreLimit(rc)
	var itemSlots chan struct{}
	_ = m.lockHub().WithResource(lockScopeScheduler, lockResourceSchedulerStoreSlots, "acquire_auto_item_store_slot", func() error {
		if m.autoItemStoreSlots == nil || m.autoItemStoreCap != itemLimit {
			m.autoItemStoreSlots = make(chan struct{}, itemLimit)
			m.autoItemStoreCap = itemLimit
		}
		itemSlots = m.autoItemStoreSlots
		return nil
	})
	select {
	case itemSlots <- struct{}{}:
	default:
		return nil, false
	}

	sharedSlots, ok := m.acquireAutoStoreSlot(rc)
	if !ok {
		m.releaseAutoStoreSlot(itemSlots)
		return nil, false
	}
	return func() {
		m.releaseAutoStoreSlot(sharedSlots)
		m.releaseAutoStoreSlot(itemSlots)
	}, true
}

func (m *RobotManager) effectiveAutoItemStoreLimit(rc robotconfig.RuntimeConfig) int {
	configLimit := normalizedStoreConcurrent(rc)
	if configLimit <= 0 {
		return 1
	}
	success := 0
	if points := m.storePoints(); points != nil {
		success = points.SuccessCount()
	}
	limit := mathx.MinInt(configLimit, 8)
	if success >= 20 {
		limit = mathx.MinInt(configLimit, mathx.MaxInt(2, success/3))
	}
	if limit < 1 {
		limit = 1
	}
	return limit
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

func (m *RobotManager) restoreAutoNormalPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) robotcap.Info {
	return m.storeMaintenance().RestoreAutoNormalPosition(info, rc, reason)
}

func (m *RobotManager) restoreAutoNormalOnline(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) (robotcap.Info, bool) {
	normal := m.restoreAutoNormalPosition(info, rc, reason)
	result, err := m.sessionService().Online(robotcap.CommandRequest{UIDs: []int{normal.UID}}, true, rc)
	recovered := err == nil && result.Confirmed == 1
	if !recovered {
		robotLogf("[AutoStore] uid=%d restore_normal_online_failed reason=%s confirmed=%d failed=%d err=%v\n",
			normal.UID, reason, result.Confirmed, result.Failed, err)
		return normal, false
	}
	robotLogf("[AutoStore] uid=%d restore_normal_online_ok reason=%s\n", normal.UID, reason)
	return normal, true
}

func (m *RobotManager) finishStoreState(uid, cid int, reason string) {
	if m == nil || uid <= 0 {
		return
	}
	m.storeMaintenance().FinishStoreState(uid, cid, reason)
}
