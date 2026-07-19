package scheduler

import (
	"time"

	robotcap "robot/internal/capability/robot"
)

const runtimeStatusCacheTTL = 2000 * time.Millisecond

// runtimeStatusMap returns an immutable snapshot. Callers that need to delete
// or replace entries must use runtimeStatusMapCopy.
func (m *RobotManager) runtimeStatusMap() map[int]robotcap.RuntimeStatus {
	for {
		now := time.Now()
		m.runtimeStatusMu.RLock()
		snapshot := m.runtimeStatusCache
		cacheAt := m.runtimeStatusCacheAt
		refreshDone := m.runtimeStatusRefresh
		m.runtimeStatusMu.RUnlock()
		if snapshot != nil && !cacheAt.IsZero() && now.Sub(cacheAt) <= runtimeStatusCacheTTL {
			return snapshot
		}
		if refreshDone != nil {
			<-refreshDone
			continue
		}

		snapshot = nil
		refreshDone = nil
		refresh := false
		m.runtimeStatusMu.Lock()
		now = time.Now()
		if m.runtimeStatusCache != nil && !m.runtimeStatusCacheAt.IsZero() && now.Sub(m.runtimeStatusCacheAt) <= runtimeStatusCacheTTL {
			snapshot = m.runtimeStatusCache
		} else if m.runtimeStatusRefresh != nil {
			refreshDone = m.runtimeStatusRefresh
		} else {
			refreshDone = make(chan struct{})
			m.runtimeStatusRefresh = refreshDone
			refresh = true
		}
		m.runtimeStatusMu.Unlock()
		if snapshot != nil {
			return snapshot
		}
		if !refresh {
			<-refreshDone
			continue
		}
		return m.refreshRuntimeStatusMap(refreshDone)
	}
}

func (m *RobotManager) refreshRuntimeStatusMap(refreshDone chan struct{}) (status map[int]robotcap.RuntimeStatus) {
	complete := false
	defer func() {
		m.runtimeStatusMu.Lock()
		if complete {
			m.runtimeStatusCache = status
			m.runtimeStatusCacheAt = time.Now()
		}
		if m.runtimeStatusRefresh == refreshDone {
			m.runtimeStatusRefresh = nil
			close(refreshDone)
		}
		m.runtimeStatusMu.Unlock()
	}()

	status = make(map[int]robotcap.RuntimeStatus)
	for _, st := range m.doll.RuntimeStatus() {
		status[st.UID] = st
	}
	complete = true
	return status
}

func (m *RobotManager) runtimeStatusMapCopy() map[int]robotcap.RuntimeStatus {
	return robotcap.CopyRuntimeStatusMap(m.runtimeStatusMap())
}

func (m *RobotManager) runtimeStatus(uid int) (robotcap.RuntimeStatus, bool) {
	if uid <= 0 {
		return robotcap.RuntimeStatus{}, false
	}
	status, ok := m.runtimeStatusMap()[uid]
	return status, ok
}

func (m *RobotManager) countRuntimeRunning() int {
	n := 0
	for _, st := range m.doll.RuntimeStatus() {
		if robotcap.ActiveRuntimeStatus(st) {
			n++
		}
	}
	return n
}
