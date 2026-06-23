package service

import "time"

func (m *RobotManager) markCleanupPending(uids []int) {
	if len(uids) == 0 {
		return
	}
	now := time.Now()
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	if m.cleanupPendingUIDs == nil {
		m.cleanupPendingUIDs = make(map[int]time.Time)
	}
	for _, uid := range uids {
		if uid > 0 {
			m.cleanupPendingUIDs[uid] = now
		}
	}
}

func (m *RobotManager) clearCleanupPending(uids []int) {
	if len(uids) == 0 {
		return
	}
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	for _, uid := range uids {
		delete(m.cleanupPendingUIDs, uid)
	}
}

func (m *RobotManager) cleanupPendingSet() map[int]bool {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	out := make(map[int]bool, len(m.cleanupPendingUIDs))
	for uid := range m.cleanupPendingUIDs {
		out[uid] = true
	}
	return out
}
