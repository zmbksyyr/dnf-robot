package service

import "time"

func (m *RobotManager) runtimeStatusMap() map[int]RuntimeRobotStatus {
	status := map[int]RuntimeRobotStatus{}
	for _, st := range m.doll.RuntimeStatus() {
		status[st.UID] = st
	}
	return status
}

func (m *RobotManager) countRuntimeRunning() int {
	n := 0
	for _, st := range m.doll.RuntimeStatus() {
		if activeRuntimeStatus(st) {
			n++
		}
	}
	return n
}

func activeRuntimeStatus(st RuntimeRobotStatus) bool {
	return st.StateName == "running" && st.DisconnectReason == 0
}

func (m *RobotManager) addAutoCreated(n int) {
	if n <= 0 {
		return
	}
	m.autoMu.Lock()
	m.autoStats.Created += n
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoOnline(success, failed int) {
	m.autoMu.Lock()
	m.autoStats.OnlineSuccess += success
	m.autoStats.OnlineFailed += failed
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoMove(success, failed int) {
	m.autoMu.Lock()
	m.autoStats.MoveSuccess += success
	m.autoStats.MoveFailed += failed
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoShoutChannel(world bool, success, failed int) {
	m.autoMu.Lock()
	if world {
		m.autoStats.ShoutWorldSuccess += success
		m.autoStats.ShoutWorldFailed += failed
	} else {
		m.autoStats.ShoutLocalSuccess += success
		m.autoStats.ShoutLocalFailed += failed
	}
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) addAutoStore(success, failed, expired int) {
	m.autoMu.Lock()
	m.autoStats.StoreSuccess += success
	m.autoStats.StoreFailed += failed
	m.autoStats.StoreExpired += expired
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoSnapshot(rc robotRuntimeConfig, running, connecting, storeRunning int) {
	m.autoMu.Lock()
	m.autoStats.Enabled = m.autoEnabled && rc.AutoActions
	m.autoStats.TargetOnline = rc.AutoTargetOnlineCount
	m.autoStats.Running = running
	m.autoStats.Connecting = connecting
	m.autoStats.StoreProbability = rc.AutoStoreProbabilityPercent
	m.autoStats.StoreRunning = storeRunning
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoActorSnapshot(actors, leased, idle, recycling, blocked int) {
	m.autoMu.Lock()
	m.autoStats.Actors = actors
	m.autoStats.Leased = leased
	m.autoStats.Idle = idle
	m.autoStats.Recycling = recycling
	m.autoStats.BlockedUIDs = blocked
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

type runtimeStatusSummary struct {
	running    int
	connecting int
	stores     int
}

func summarizeRuntimeStatusSlice(status []RuntimeRobotStatus) (running, connecting, stores int) {
	var summary runtimeStatusSummary
	for _, st := range status {
		summary.add(st)
	}
	return summary.running, summary.connecting, summary.stores
}

func summarizeRuntimeStatusMap(status map[int]RuntimeRobotStatus) (running, connecting, stores int) {
	var summary runtimeStatusSummary
	for _, st := range status {
		summary.add(st)
	}
	return summary.running, summary.connecting, summary.stores
}

func (s *runtimeStatusSummary) add(st RuntimeRobotStatus) {
	if st.DisconnectReason != 0 {
		return
	}
	switch st.StateName {
	case "running":
		s.running++
		if (st.RobotType == 2 || st.RobotType == 3) && st.StoreDisplayAck {
			s.stores++
		}
	case "init", "login":
		s.connecting++
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
