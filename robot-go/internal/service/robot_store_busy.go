package service

func (m *RobotManager) beginStoreBusy(uid int) bool {
	if uid <= 0 {
		return false
	}
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.autoStoreBusy == nil {
		m.autoStoreBusy = make(map[int]bool)
	}
	if m.autoStoreBusy[uid] {
		return false
	}
	m.autoStoreBusy[uid] = true
	return true
}

func (m *RobotManager) endStoreBusy(uid int) {
	if uid <= 0 {
		return
	}
	m.autoMu.Lock()
	delete(m.autoStoreBusy, uid)
	m.autoMu.Unlock()
}
