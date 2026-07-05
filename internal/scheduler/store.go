package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	storecap "robot/internal/capability/store"
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
	limit := rc.SchedulerStoreConcurrent
	if limit <= 0 {
		limit = 30
	}
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

func (m *RobotManager) finishStoreState(uid, cid int, reason string) {
	if m == nil || uid <= 0 {
		return
	}
	m.storeMaintenance().FinishStoreState(uid, cid, reason)
}
