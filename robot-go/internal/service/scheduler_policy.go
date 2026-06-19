package service

import (
	"fmt"
	"sort"
	"time"
)

type schedulerPolicyMode string

const (
	schedulerPolicyBootstrap   schedulerPolicyMode = "bootstrap"
	schedulerPolicyFill        schedulerPolicyMode = "fill"
	schedulerPolicyStable      schedulerPolicyMode = "stable"
	schedulerPolicyStore       schedulerPolicyMode = "store_expand"
	schedulerPolicyPressure    schedulerPolicyMode = "pressure"
	schedulerPolicyBreaker     schedulerPolicyMode = "breaker"
	schedulerPolicyMaintenance schedulerPolicyMode = "maintenance"
)

type adaptiveSchedulerSignals struct {
	Live           bool
	Running        int
	Connecting     int
	StoreRunning   int
	Actors         int
	Idle           int
	ActorIdle      int
	ActorAssigned  int
	ActorOnline    int
	ActorRunning   int
	ActorBusy      int
	ActorReleasing int
	GamePortReady  bool
	BreakerActive  bool
	CPUPercent     float64
	MemoryMB       int
	Goroutines     int
}

type schedulerPolicyDecision struct {
	Mode   schedulerPolicyMode
	Reason string
}

func (m *RobotManager) applyAdaptiveSchedulerConfig(rc *robotRuntimeConfig) {
	signals := m.adaptiveSchedulerSignals()
	decision := applyAdaptiveSchedulerConfig(rc, signals)
	m.updateSchedulerStatus(*rc, signals, decision)
	m.logSchedulerPolicyDecision(decision)
}

func (m *RobotManager) SchedulerStatus() SchedulerStatus {
	rc := m.loadRobotConfig()
	signals := m.adaptiveSchedulerSignals()
	decision := applyAdaptiveSchedulerConfig(&rc, signals)
	if !m.autoActionsEnabled(rc) {
		decision = schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: "auto_disabled"}
	}
	m.updateSchedulerStatus(rc, signals, decision)

	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.schedulerStatus
}

func (m *RobotManager) adaptiveSchedulerSignals() adaptiveSchedulerSignals {
	now := time.Now()
	m.autoMu.Lock()
	stats := m.autoStats
	live := !stats.UpdatedAt.IsZero()
	breaker := now.Before(m.autoBreakerUntil)
	m.autoMu.Unlock()

	cpu, mem, threads := robotResourceSnapshot()
	return adaptiveSchedulerSignals{
		Live:           live,
		Running:        stats.Running,
		Connecting:     stats.Connecting,
		StoreRunning:   stats.StoreRunning,
		Actors:         stats.Actors,
		Idle:           stats.Idle,
		ActorIdle:      stats.ActorIdle,
		ActorAssigned:  stats.ActorAssigned,
		ActorOnline:    stats.ActorOnline,
		ActorRunning:   stats.ActorRunning,
		ActorBusy:      stats.ActorBusy,
		ActorReleasing: stats.ActorReleasing,
		GamePortReady:  !live || stats.GamePortReady,
		BreakerActive:  breaker,
		CPUPercent:     cpu,
		MemoryMB:       mem,
		Goroutines:     threads,
	}
}

func (m *RobotManager) logSchedulerPolicyDecision(decision schedulerPolicyDecision) {
	if decision.Mode == "" {
		return
	}
	m.autoMu.Lock()
	lastMode := m.autoPolicyLastMode
	if lastMode == decision.Mode {
		m.autoMu.Unlock()
		return
	}
	m.autoPolicyLastMode = decision.Mode
	m.autoPolicyLastReason = decision.Reason
	m.autoMu.Unlock()
	robotLogf("[SchedulerPolicy] mode=%s reason=%s\n", decision.Mode, decision.Reason)
}

func (m *RobotManager) updateSchedulerStatus(rc robotRuntimeConfig, sig adaptiveSchedulerSignals, decision schedulerPolicyDecision) {
	target := normalizedTarget(rc)
	op, opStarted, opActive := m.structuralOperation()
	if opActive {
		decision = schedulerPolicyDecision{Mode: schedulerPolicyMaintenance, Reason: "structural_op=" + op}
	}
	recent := m.RecentOperation()
	status := SchedulerStatus{
		Mode:                    string(decision.Mode),
		Reason:                  decision.Reason,
		RecentOperation:         recent.Type,
		RecentOperationState:    recent.State,
		RecentOperationSummary:  recentOperationSummary(recent),
		TargetOnline:            target,
		Running:                 sig.Running,
		Connecting:              sig.Connecting,
		Actors:                  sig.Actors,
		Idle:                    sig.Idle,
		ActorIdle:               sig.ActorIdle,
		ActorAssigned:           sig.ActorAssigned,
		ActorOnline:             sig.ActorOnline,
		ActorRunning:            sig.ActorRunning,
		ActorBusy:               sig.ActorBusy,
		ActorReleasing:          sig.ActorReleasing,
		StoreRunning:            sig.StoreRunning,
		GamePortReady:           sig.GamePortReady,
		BreakerActive:           sig.BreakerActive,
		CPUPercent:              sig.CPUPercent,
		MemoryMB:                sig.MemoryMB,
		Goroutines:              sig.Goroutines,
		OnlineBatchSize:         rc.SchedulerOnlineBatchSize,
		OnlineStartRate:         rc.SchedulerOnlineStartRate,
		OnlineFillTimeoutSec:    rc.SchedulerOnlineFillTimeout,
		StoreConcurrent:         rc.SchedulerStoreConcurrent,
		StoreProbabilityPercent: rc.AutoStoreProbabilityPercent,
		StoreIntervalMinSec:     rc.AutoStoreIntervalMinSec,
		StoreIntervalMaxSec:     rc.AutoStoreIntervalMaxSec,
		StoreMaxPositionTries:   rc.AutoStoreMaxPositionTries,
		ScaleUpBatch:            schedulerScaleUpBatch(rc),
		ScaleDownBatch:          schedulerScaleDownBatch(sig.Actors, target),
		BreakerReleaseBatch:     rc.SchedulerBreakerReleaseBatch,
		PortDownReleaseBatch:    rc.SchedulerPortDownReleaseBatch,
		OperationActive:         opActive,
		Operation:               op,
		OperationStartedAt:      opStarted,
		UpdatedAt:               time.Now(),
	}
	m.autoMu.Lock()
	m.schedulerStatus = status
	m.autoMu.Unlock()
}

func recentOperationSummary(op RobotOperationStatus) string {
	if op.ID <= 0 {
		return ""
	}
	if op.Error != "" {
		return op.Error
	}
	return op.Summary
}

func applyAdaptiveSchedulerConfig(rc *robotRuntimeConfig, sig adaptiveSchedulerSignals) schedulerPolicyDecision {
	target := normalizedTarget(*rc)
	scale := targetScale(target)

	rc.SchedulerOnlineBatchSize = clampInt(target/10, 10, 60)
	rc.SchedulerOnlineStartRate = clampInt(target/40, 4, 30)
	rc.SchedulerOnlineFillTimeout = clampInt(target/10, 45, 180)

	rc.SchedulerStoreConcurrent = clampInt(target/20, 5, 50)
	rc.AutoStoreProbabilityPercent = clampInt(100/scale, 5, 35)
	rc.AutoStoreIntervalMinSec = clampInt(45+scale*5, 50, 120)
	rc.AutoStoreIntervalMaxSec = rc.AutoStoreIntervalMinSec + clampInt(60+scale*10, 90, 240)
	rc.AutoStoreDurationSec = clampInt(120+scale*15, 120, 300)
	rc.AutoStoreTickSec = clampInt(5+scale, 10, 30)
	rc.AutoStoreMaxPositionTries = clampInt(4+scale, 5, 20)
	rc.AutoStoreFailCooldownSec = clampInt(60+scale*15, 60, 240)

	rc.SchedulerBreakerAbnormalPct = 30
	rc.SchedulerBreakerPauseSec = clampInt(120+scale*30, 180, 600)
	rc.SchedulerBreakerReleaseBatch = clampInt(target/30, 5, 40)
	rc.SchedulerBreakerFloorPct = 70
	rc.SchedulerPortDownReleaseBatch = clampInt(target/25, 5, 50)

	if !sig.Live {
		return schedulerPolicyDecision{Mode: schedulerPolicyBootstrap, Reason: fmt.Sprintf("target=%d no_live_snapshot", target)}
	}
	return applyLiveSchedulerFeedback(rc, target, sig)
}

func applyLiveSchedulerFeedback(rc *robotRuntimeConfig, target int, sig adaptiveSchedulerSignals) schedulerPolicyDecision {
	if target <= 0 {
		return schedulerPolicyDecision{Mode: schedulerPolicyStable, Reason: "target_zero"}
	}
	connectingLimit := clampInt(target/20, 2, 30)
	healthyOnline := sig.Running >= target*95/100 && sig.Connecting <= connectingLimit && sig.GamePortReady && !sig.BreakerActive
	storeRoom := sig.StoreRunning < rc.SchedulerStoreConcurrent*7/10
	idleRoom := sig.Idle >= clampInt(target/50, 2, 20)
	resourcePressure := sig.CPUPercent >= 85 || sig.MemoryMB >= 4096 || sig.Goroutines >= 20000
	pendingPressure := sig.Idle >= schedulerPendingActorLimit(target, *rc)
	connectionPressure := sig.Connecting > clampInt(target/10, 3, 60)
	pressure := sig.BreakerActive || !sig.GamePortReady || resourcePressure || connectionPressure || pendingPressure

	if pressure {
		if pendingPressure && sig.GamePortReady && !sig.BreakerActive && !resourcePressure && !connectionPressure {
			rc.SchedulerOnlineBatchSize = -1
			rc.SchedulerOnlineStartRate = clampInt(rc.SchedulerOnlineStartRate/2, 1, 10)
			rc.SchedulerStoreConcurrent = clampInt(rc.SchedulerStoreConcurrent/2, 2, 25)
			rc.AutoStoreProbabilityPercent = clampInt(rc.AutoStoreProbabilityPercent/3, 1, 15)
			return schedulerPolicyDecision{Mode: schedulerPolicyPressure, Reason: fmt.Sprintf("pending_backlog idle=%d actors=%d running=%d target=%d", sig.Idle, sig.Actors, sig.Running, target)}
		}
		rc.SchedulerOnlineBatchSize = clampInt(rc.SchedulerOnlineBatchSize/2, 5, 60)
		rc.SchedulerOnlineStartRate = clampInt(rc.SchedulerOnlineStartRate/2, 4, 30)
		rc.SchedulerOnlineFillTimeout = clampInt(rc.SchedulerOnlineFillTimeout*2, 60, 300)
		rc.SchedulerStoreConcurrent = clampInt(rc.SchedulerStoreConcurrent/2, 2, 25)
		rc.AutoStoreProbabilityPercent = clampInt(rc.AutoStoreProbabilityPercent/3, 1, 15)
		rc.AutoStoreIntervalMinSec = clampInt(rc.AutoStoreIntervalMinSec*3/2, 60, 240)
		rc.AutoStoreIntervalMaxSec = clampInt(rc.AutoStoreIntervalMaxSec*3/2, rc.AutoStoreIntervalMinSec+60, 480)
		rc.AutoStoreFailCooldownSec = clampInt(rc.AutoStoreFailCooldownSec*2, 120, 600)
		rc.SchedulerBreakerReleaseBatch = clampInt(rc.SchedulerBreakerReleaseBatch*3/2, 5, 60)
		rc.SchedulerPortDownReleaseBatch = clampInt(rc.SchedulerPortDownReleaseBatch*3/2, 5, 80)
		mode := schedulerPolicyPressure
		if sig.BreakerActive {
			mode = schedulerPolicyBreaker
		}
		return schedulerPolicyDecision{Mode: mode, Reason: fmt.Sprintf("running=%d connecting=%d store=%d cpu=%.1f mem=%d goroutines=%d port=%v", sig.Running, sig.Connecting, sig.StoreRunning, sig.CPUPercent, sig.MemoryMB, sig.Goroutines, sig.GamePortReady)}
	}

	if healthyOnline && storeRoom {
		storeBoost := clampInt(target/40, 2, 25)
		if idleRoom {
			storeBoost += clampInt(sig.Idle/2, 1, 20)
		}
		rc.SchedulerStoreConcurrent = clampInt(rc.SchedulerStoreConcurrent+storeBoost, 5, 90)
		rc.AutoStoreProbabilityPercent = clampInt(rc.AutoStoreProbabilityPercent+10, 5, 60)
		rc.AutoStoreIntervalMinSec = clampInt(rc.AutoStoreIntervalMinSec*4/5, 30, 120)
		rc.AutoStoreIntervalMaxSec = clampInt(rc.AutoStoreIntervalMaxSec*4/5, rc.AutoStoreIntervalMinSec+60, 300)
		rc.AutoStoreMaxPositionTries = clampInt(rc.AutoStoreMaxPositionTries+3, 5, 30)
		return schedulerPolicyDecision{Mode: schedulerPolicyStore, Reason: fmt.Sprintf("running=%d idle=%d store=%d store_limit=%d", sig.Running, sig.Idle, sig.StoreRunning, rc.SchedulerStoreConcurrent)}
	}

	if sig.Running+sig.Connecting < target && sig.Connecting <= connectingLimit {
		rc.SchedulerOnlineBatchSize = clampInt(rc.SchedulerOnlineBatchSize+target/20, 20, 120)
		rc.SchedulerOnlineStartRate = clampInt(rc.SchedulerOnlineStartRate+target/100, 8, 60)
		return schedulerPolicyDecision{Mode: schedulerPolicyFill, Reason: fmt.Sprintf("running=%d connecting=%d target=%d", sig.Running, sig.Connecting, target)}
	}
	return schedulerPolicyDecision{Mode: schedulerPolicyStable, Reason: fmt.Sprintf("running=%d connecting=%d store=%d", sig.Running, sig.Connecting, sig.StoreRunning)}
}

func schedulerTargetCapacity(rc robotRuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	return target
}

func schedulerCreateRoom(rc robotRuntimeConfig, existing int) int {
	room := schedulerTargetCapacity(rc) - existing
	if room < 0 {
		return 0
	}
	return room
}

func schedulerScaleUpBatch(rc robotRuntimeConfig) int {
	batch := rc.SchedulerOnlineBatchSize
	if batch < 0 {
		return 0
	}
	if batch <= 0 {
		batch = 20
	}
	if batch > 120 {
		batch = 120
	}
	return batch
}

func schedulerPendingActorLimit(target int, rc robotRuntimeConfig) int {
	if target <= 0 {
		return 1
	}
	limit := schedulerOnlineStartRate(rc) * 8
	if limit < target/10 {
		limit = target / 10
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 120 {
		limit = 120
	}
	return limit
}

func schedulerScaleDownBatch(current, target int) int {
	delta := current - target
	if delta <= 0 {
		return 0
	}
	batch := current / 25
	if current%25 != 0 {
		batch++
	}
	if batch < 5 {
		batch = 5
	}
	if batch > 50 {
		batch = 50
	}
	if batch > delta {
		batch = delta
	}
	return batch
}

func schedulerOnlineStartRate(rc robotRuntimeConfig) int {
	rate := rc.SchedulerOnlineStartRate
	if rate < 0 {
		return 0
	}
	if rate <= 0 {
		rate = 20
	}
	if rate > 60 {
		rate = 60
	}
	return rate
}

func schedulerOnlineStartRateForNeed(need int, rc robotRuntimeConfig) int {
	rate := schedulerOnlineStartRate(rc)
	if rate <= 0 {
		return 0
	}
	if need <= 0 {
		return rate
	}
	timeout := rc.SchedulerOnlineFillTimeout
	if timeout <= 0 {
		timeout = 60
	}
	required := (need + timeout - 1) / timeout
	if required > rate {
		rate = required
	}
	if rate > 60 {
		return 60
	}
	return rate
}

func breakerActorFloor(rc robotRuntimeConfig) int {
	target := schedulerTargetCapacity(rc)
	floorPct := rc.SchedulerBreakerFloorPct
	if floorPct < 0 {
		floorPct = 0
	}
	if floorPct > 100 {
		floorPct = 100
	}
	floor := target * floorPct / 100
	if target <= 50 && floor < target {
		return target
	}
	return floor
}

func sortActorsForStopByPolicy(actors []*robotActor, status map[int]RuntimeRobotStatus) {
	sort.Slice(actors, func(i, j int) bool {
		leftPriority := actorStopPriority(actors[i], status)
		rightPriority := actorStopPriority(actors[j], status)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftUID := actors[i].uidValue()
		rightUID := actors[j].uidValue()
		if leftUID <= 0 || rightUID <= 0 {
			if leftUID != rightUID {
				return leftUID <= 0
			}
			return actors[i].slotID > actors[j].slotID
		}
		if leftUID != rightUID {
			return leftUID > rightUID
		}
		return actors[i].slotID > actors[j].slotID
	})
}

func actorStopPriority(actor *robotActor, status map[int]RuntimeRobotStatus) int {
	uid := actor.uidValue()
	if uid <= 0 {
		return 0
	}
	st, ok := status[uid]
	if !ok || st.DisconnectReason != 0 || st.StateName == "init" || st.StateName == "login" {
		return 1
	}
	if st.RobotType == 2 || st.RobotType == 3 || st.StoreDisplayAck {
		return 2
	}
	return 3
}

func normalizedTarget(rc robotRuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	if target <= 0 {
		target = 20
	}
	return target
}

func targetScale(target int) int {
	scale := target / 100
	if target%100 != 0 {
		scale++
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 10 {
		scale = 10
	}
	return scale
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
