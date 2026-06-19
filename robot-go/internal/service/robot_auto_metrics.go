package service

import (
	"fmt"
	"time"

	"robot/internal/dnf"
)

// Metrics and status aggregation.

func (s *RobotSupervisor) updateMetrics(rc robotRuntimeConfig) {
	now := time.Now()
	if !s.nextMetrics.IsZero() && now.Before(s.nextMetrics) {
		return
	}
	s.nextMetrics = now.Add(time.Duration(rc.SchedulerMetricsIntervalSec) * time.Second)
	status := s.manager.runtimeStatusMap()
	s.filterBlockedRuntimeStatus(status)
	s.filterMissingRuntimeStatus(status)
	running, connecting, stores := summarizeRuntimeStatusMap(status)
	s.manager.updateAutoSnapshot(rc, running, connecting, stores)
	counts := s.actorCounts(now, rc)
	s.manager.updateAutoActorSnapshot(counts)
	s.manager.updateAutoBreaker(now, rc, counts, running, connecting)
	cpu, mem, threads := robotResourceSnapshot()
	s.manager.autoMu.Lock()
	stats := s.manager.autoStats
	policy := s.manager.schedulerStatus
	s.manager.autoMu.Unlock()
	line := fmt.Sprintf("[RobotMetrics] policy=%s target=%d actors=%d leased=%d idle=%d state idle=%d assigned=%d online=%d running=%d busy=%d releasing=%d runtime running=%d store=%d connecting=%d recycling=%d blocked=%d cpu=%.1f mem_mb=%d goroutines=%d online=%d/%d move=%d/%d shout_local=%d/%d shout_world=%d/%d store=%d/%d expired=%d\n",
		policy.Mode,
		rc.AutoTargetOnlineCount, counts.auto, counts.leased, counts.idle,
		counts.stateIdle, counts.stateAssigned, counts.stateOnline, counts.stateRunning, counts.stateBusy, counts.stateReleasing,
		running, stores, connecting, counts.releasing, counts.blocked,
		cpu, mem, threads,
		stats.OnlineSuccess, stats.OnlineFailed,
		stats.MoveSuccess, stats.MoveFailed,
		stats.ShoutLocalSuccess, stats.ShoutLocalFailed,
		stats.ShoutWorldSuccess, stats.ShoutWorldFailed,
		stats.StoreSuccess, stats.StoreFailed, stats.StoreExpired)
	fmt.Print(line)
	dnf.LogString(dnf.LogLevelIndispensable, line)
}

func (s *RobotSupervisor) filterBlockedRuntimeStatus(status map[int]RuntimeRobotStatus) {
	if len(status) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for uid := range s.blockedUID {
		delete(status, uid)
	}
}

func (s *RobotSupervisor) filterMissingRuntimeStatus(status map[int]RuntimeRobotStatus) {
	if len(status) == 0 {
		return
	}
	uids := make([]int, 0, len(status))
	for uid := range status {
		uids = append(uids, uid)
	}
	alive, err := s.manager.aliveRobotUIDs(uids)
	if err != nil {
		robotLogf("[RobotSupervisor] runtime_alive_filter_failed err=%v\n", err)
		return
	}
	for uid := range status {
		if !alive[uid] {
			delete(status, uid)
		}
	}
}

type supervisorActorCounts struct {
	auto           int
	leased         int
	idle           int
	releasing      int
	blocked        int
	stateIdle      int
	stateAssigned  int
	stateOnline    int
	stateRunning   int
	stateBusy      int
	stateReleasing int
}

func (s *RobotSupervisor) actorCounts(now time.Time, rc robotRuntimeConfig) supervisorActorCounts {
	counts := supervisorActorCounts{blocked: s.actorLedger.blockedCount()}
	for _, actor := range s.actorLedger.autoActorPointers() {
		status := actor.status(now, rc)
		counts.auto++
		if status.UID > 0 {
			counts.leased++
		} else {
			counts.idle++
		}
		if status.State == robotActorReleasing {
			counts.releasing++
		}
		switch status.State {
		case robotActorIdle:
			counts.stateIdle++
		case robotActorOffline:
			counts.stateAssigned++
		case robotActorAssigned:
			counts.stateAssigned++
		case robotActorOnline:
			counts.stateOnline++
		case robotActorRunning:
			counts.stateRunning++
		case robotActorBusy:
			counts.stateBusy++
		case robotActorReleasing:
			counts.stateReleasing++
		}
	}
	return counts
}
