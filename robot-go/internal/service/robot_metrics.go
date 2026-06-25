package service

import (
	"fmt"
	"time"
)

const runtimeStatusCacheTTL = 2000 * time.Millisecond

func (m *RobotManager) runtimeStatusMap() map[int]RuntimeRobotStatus {
	now := time.Now()
	m.runtimeStatusMu.Lock()
	if !m.runtimeStatusCacheAt.IsZero() && now.Sub(m.runtimeStatusCacheAt) <= runtimeStatusCacheTTL && m.runtimeStatusCache != nil {
		out := copyRuntimeStatusMap(m.runtimeStatusCache)
		m.runtimeStatusMu.Unlock()
		return out
	}
	m.runtimeStatusMu.Unlock()

	status := map[int]RuntimeRobotStatus{}
	for _, st := range m.doll.RuntimeStatus() {
		status[st.UID] = st
	}
	m.runtimeStatusMu.Lock()
	m.runtimeStatusCache = copyRuntimeStatusMap(status)
	m.runtimeStatusCacheAt = now
	m.runtimeStatusMu.Unlock()
	return status
}

func copyRuntimeStatusMap(in map[int]RuntimeRobotStatus) map[int]RuntimeRobotStatus {
	out := make(map[int]RuntimeRobotStatus, len(in))
	for uid, st := range in {
		out[uid] = st
	}
	return out
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

func (m *RobotManager) updateAutoActorSnapshot(counts actorLedgerCounts) {
	m.autoMu.Lock()
	m.autoStats.Actors = counts.auto
	m.autoStats.Leased = counts.leased
	m.autoStats.Idle = counts.idle
	m.autoStats.Recycling = counts.releasing
	m.autoStats.BlockedUIDs = counts.blocked
	m.autoStats.ActorIdle = counts.stateIdle
	m.autoStats.ActorAssigned = counts.stateAssigned
	m.autoStats.ActorOnline = counts.stateOnline
	m.autoStats.ActorRunning = counts.stateRunning
	m.autoStats.ActorBusy = counts.stateBusy
	m.autoStats.ActorReleasing = counts.stateReleasing
	m.autoStats.UpdatedAt = time.Now()
	m.autoMu.Unlock()
}

func (m *RobotManager) updateAutoBreaker(now time.Time, rc robotRuntimeConfig, counts actorLedgerCounts, running, connecting int) {
	target := rc.AutoTargetOnlineCount
	if target <= 0 {
		return
	}
	if target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	abnormalPct := rc.SchedulerBreakerAbnormalPct
	if abnormalPct <= 0 {
		abnormalPct = 30
	}
	if abnormalPct > 100 {
		abnormalPct = 100
	}
	threshold := (target*abnormalPct + 99) / 100
	readyForBreaker := counts.auto >= (target*9+9)/10 || counts.leased >= (target*9+9)/10

	m.autoMu.Lock()
	defer m.autoMu.Unlock()

	stats := m.autoStats
	reason := ""
	if readyForBreaker && connecting >= threshold {
		reason = fmt.Sprintf("connecting_over_%dpct target=%d connecting=%d", abnormalPct, target, connecting)
	}

	if m.autoBreakerLastCheck.IsZero() || now.Sub(m.autoBreakerLastCheck) >= time.Minute {
		failDelta := (stats.OnlineFailed - m.autoBreakerLastOnlineFailed) +
			(stats.MoveFailed - m.autoBreakerLastMoveFailed) +
			(stats.ShoutLocalFailed - m.autoBreakerLastShoutLocalFailed) +
			(stats.ShoutWorldFailed - m.autoBreakerLastShoutWorldFailed) +
			(stats.StoreFailed - m.autoBreakerLastStoreFailed)
		m.autoBreakerLastCheck = now
		m.autoBreakerLastOnlineFailed = stats.OnlineFailed
		m.autoBreakerLastMoveFailed = stats.MoveFailed
		m.autoBreakerLastShoutLocalFailed = stats.ShoutLocalFailed
		m.autoBreakerLastShoutWorldFailed = stats.ShoutWorldFailed
		m.autoBreakerLastStoreFailed = stats.StoreFailed
		if readyForBreaker && failDelta >= threshold {
			reason = fmt.Sprintf("failures_over_%dpct_per_min target=%d failed_delta=%d", abnormalPct, target, failDelta)
		}
	}

	if reason == "" {
		return
	}
	pauseSec := rc.SchedulerBreakerPauseSec
	if pauseSec <= 0 {
		pauseSec = 300
	}
	until := now.Add(time.Duration(pauseSec) * time.Second)
	wasActive := now.Before(m.autoBreakerUntil)
	if until.After(m.autoBreakerUntil) {
		m.autoBreakerUntil = until
	}
	if !wasActive || m.autoBreakerReason != reason {
		m.autoBreakerReason = reason
		robotLogf("[AutoBreaker] pause_until=%s reason=%s running=%d connecting=%d actors=%d leased=%d failed online=%d move=%d local=%d world=%d store=%d\n",
			m.autoBreakerUntil.Format(time.RFC3339), reason, running, connecting, counts.auto, counts.leased,
			stats.OnlineFailed, stats.MoveFailed, stats.ShoutLocalFailed, stats.ShoutWorldFailed, stats.StoreFailed)
	}
}

func (m *RobotManager) autoBreakerActive(now time.Time) bool {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return now.Before(m.autoBreakerUntil)
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
