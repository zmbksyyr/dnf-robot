package scheduler

import (
	robotcap "robot/internal/capability/robot"
	"testing"
	"time"

	actormodel "robot/internal/actor"
	robotconfig "robot/internal/capability/robotconfig"
)

func TestSchedulerPolicyReasonConstants(t *testing.T) {
	tests := []struct {
		got  string
		want string
	}{
		{schedulerReasonAutoDisabled, "auto_disabled"},
		{schedulerReasonGamePortUnstable, "game_port_unstable"},
		{schedulerReasonBreakerActive, "breaker_active"},
		{schedulerReasonTargetZero, "target_zero"},
		{schedulerReasonNoLiveSnapshot, "no_live_snapshot"},
		{schedulerReasonKeyInvalid, "key_invalid"},
		{schedulerReasonKeyInvalidPrefix, "key_invalid="},
		{schedulerReasonStructuralPrefix, "structural_op="},
		{schedulerReasonActorPrefix, "actor_container="},
		{schedulerReasonPendingBacklog, "pending_backlog"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("scheduler policy reason constant got %q want %q", tt.got, tt.want)
		}
	}
}

func TestSortActorsForStopByPolicy(t *testing.T) {
	actors := []*actormodel.Actor{
		testRobotActor(1, actormodel.ModeAuto, 17000001),
		testRobotActor(2, actormodel.ModeAuto, 17000002),
		testRobotActor(3, actormodel.ModeAuto, 17000003),
		testRobotActor(4, actormodel.ModeAuto, 0),
	}
	status := map[int]robotcap.RuntimeStatus{
		17000001: {UID: 17000001, StateName: robotcap.RuntimeStateRunning, DisconnectReason: 0},
		17000002: {UID: 17000002, StateName: robotcap.RuntimeStateRunning, DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		17000003: {UID: 17000003, StateName: robotcap.RuntimeStateLogin, DisconnectReason: 0},
	}
	sortActorsForStopByPolicy(actors, status)
	got := []int{actors[0].UIDValue(), actors[1].UIDValue(), actors[2].UIDValue(), actors[3].UIDValue()}
	assertIntSlice(t, got, []int{0, 17000003, 17000002, 17000001})
}

func TestBeginAdaptiveStoreTypeBalancesPlannedStores(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.runtimeStatusCache = map[int]robotcap.RuntimeStatus{
		1: {UID: 1, StateName: robotcap.RuntimeStateRunning, RobotType: 2, StoreDisplayAck: true},
		2: {UID: 2, StateName: robotcap.RuntimeStateRunning, RobotType: 2, StoreDisplayAck: true},
		3: {UID: 3, StateName: robotcap.RuntimeStateRunning, RobotType: 3, DisjointActive: true},
	}
	m.runtimeStatusCacheAt = time.Now()

	disjoint, done := m.beginAdaptiveStoreType()
	if !disjoint {
		t.Fatalf("expected disjoint store when item stores are ahead")
	}
	done()

	m.runtimeStatusCache = map[int]robotcap.RuntimeStatus{
		1: {UID: 1, StateName: robotcap.RuntimeStateRunning, RobotType: 2, StoreDisplayAck: true},
		2: {UID: 2, StateName: robotcap.RuntimeStateRunning, RobotType: 3, DisjointActive: true},
	}
	firstDisjoint, firstDone := m.beginAdaptiveStoreType()
	secondDisjoint, secondDone := m.beginAdaptiveStoreType()
	defer firstDone()
	defer secondDone()
	if firstDisjoint || !secondDisjoint {
		t.Fatalf("balanced stores should plan item then disjoint, got first=%v second=%v", firstDisjoint, secondDisjoint)
	}
}

func TestLoadRobotConfigSchedulerOnlineDefaults(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 10 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 10", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 4 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 4", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 45 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 45", rc.SchedulerOnlineFillTimeout)
	}
	if rc.SchedulerBreakerAbnormalPct != 30 {
		t.Fatalf("SchedulerBreakerAbnormalPct got %d want 30", rc.SchedulerBreakerAbnormalPct)
	}
	if rc.SchedulerBreakerPauseSec != 180 {
		t.Fatalf("SchedulerBreakerPauseSec got %d want 180", rc.SchedulerBreakerPauseSec)
	}
	if rc.SchedulerBreakerReleaseBatch != 5 {
		t.Fatalf("SchedulerBreakerReleaseBatch got %d want 5", rc.SchedulerBreakerReleaseBatch)
	}
	if rc.SchedulerBreakerFloorPct != 70 {
		t.Fatalf("SchedulerBreakerFloorPct got %d want 70", rc.SchedulerBreakerFloorPct)
	}
	if rc.SchedulerPortDownReleaseBatch != 5 {
		t.Fatalf("SchedulerPortDownReleaseBatch got %d want 5", rc.SchedulerPortDownReleaseBatch)
	}
	assertIntSlice(t, rc.EquipSlots, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	if rc.EquipIntensifyMin != 7 || rc.EquipIntensifyMax != 10 {
		t.Fatalf("EquipIntensify got %d..%d want 7..10", rc.EquipIntensifyMin, rc.EquipIntensifyMax)
	}
	if !rc.AutoActions || rc.AutoTargetOnlineCount != 20 || rc.AutoStoreProbabilityPercent != 35 {
		t.Fatalf("auto defaults got enabled=%v target=%d store_probability=%d, want true/20/35", rc.AutoActions, rc.AutoTargetOnlineCount, rc.AutoStoreProbabilityPercent)
	}
	if rc.AutoMoveIntervalMinSec != 6 || rc.AutoMoveIntervalMaxSec != 18 {
		t.Fatalf("AutoMoveInterval got %d..%d want 6..18", rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec)
	}
	if rc.AutoShoutIntervalMinSec != 40 || rc.AutoShoutIntervalMaxSec != 115 {
		t.Fatalf("AutoShoutInterval got %d..%d want 40..115", rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
	}
	assertIntSlice(t, rc.StoreItemAllowIDs, []int{3037, 3031, 3032, 3034, 3035})
}

func TestLoadRobotConfigSchedulerValuesAreAdaptive(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[auto]\nauto_target_online_count = 600\nauto_store_probability_percent = 99\nauto_store_interval_min_sec = 1\nauto_store_interval_max_sec = 2\n[scheduler]\nstore_concurrent = 1\nonline_batch_size = 30\nonline_start_rate = 8\nonline_fill_timeout_sec = 90\nbreaker_abnormal_percent = 25\nbreaker_pause_sec = 120\nbreaker_release_batch = 10\nbreaker_floor_percent = 80\nport_down_release_batch = 15\n")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 60 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 60", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 15 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 15", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 60 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 60", rc.SchedulerOnlineFillTimeout)
	}
	if rc.SchedulerStoreConcurrent != 30 {
		t.Fatalf("SchedulerStoreConcurrent got %d want 30", rc.SchedulerStoreConcurrent)
	}
	if rc.AutoStoreProbabilityPercent != 16 {
		t.Fatalf("AutoStoreProbabilityPercent got %d want 16", rc.AutoStoreProbabilityPercent)
	}
	if rc.AutoMoveIntervalMinSec != 11 || rc.AutoMoveIntervalMaxSec != 33 {
		t.Fatalf("AutoMoveInterval got %d..%d want 11..33", rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec)
	}
	if rc.AutoShoutIntervalMinSec != 65 || rc.AutoShoutIntervalMaxSec != 185 {
		t.Fatalf("AutoShoutInterval got %d..%d want 65..185", rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
	}
	if rc.AutoStoreIntervalMinSec != 75 || rc.AutoStoreIntervalMaxSec != 195 {
		t.Fatalf("AutoStoreInterval got %d..%d want 75..195", rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
	}
	if rc.SchedulerBreakerAbnormalPct != 30 {
		t.Fatalf("SchedulerBreakerAbnormalPct got %d want 30", rc.SchedulerBreakerAbnormalPct)
	}
	if rc.SchedulerBreakerPauseSec != 300 {
		t.Fatalf("SchedulerBreakerPauseSec got %d want 300", rc.SchedulerBreakerPauseSec)
	}
	if rc.SchedulerBreakerReleaseBatch != 20 {
		t.Fatalf("SchedulerBreakerReleaseBatch got %d want 20", rc.SchedulerBreakerReleaseBatch)
	}
	if rc.SchedulerBreakerFloorPct != 70 {
		t.Fatalf("SchedulerBreakerFloorPct got %d want 70", rc.SchedulerBreakerFloorPct)
	}
	if rc.SchedulerPortDownReleaseBatch != 24 {
		t.Fatalf("SchedulerPortDownReleaseBatch got %d want 24", rc.SchedulerPortDownReleaseBatch)
	}
}

func TestLoadRobotConfigDoesNotAutoPatchEquipSlots(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[equipment]\nequip_slots = 3,4,5\n")
	rc := m.loadRobotConfig()
	assertIntSlice(t, rc.EquipSlots, []int{3, 4, 5})
}

func TestLoadRobotConfigSchedulerAdaptiveCaps(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[auto]\nauto_target_online_count = 2000\n[scheduler]\nonline_batch_size = 999\nonline_start_rate = 999\nonline_fill_timeout_sec = -1\nbreaker_abnormal_percent = 999\nbreaker_pause_sec = 9999\nbreaker_release_batch = 999\nbreaker_floor_percent = 999\nport_down_release_batch = 999\n")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 60 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 60", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 25 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 25", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 100 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 100", rc.SchedulerOnlineFillTimeout)
	}
	if rc.SchedulerBreakerAbnormalPct != 30 {
		t.Fatalf("SchedulerBreakerAbnormalPct got %d want 30", rc.SchedulerBreakerAbnormalPct)
	}
	if rc.SchedulerBreakerPauseSec != 420 {
		t.Fatalf("SchedulerBreakerPauseSec got %d want 420", rc.SchedulerBreakerPauseSec)
	}
	if rc.SchedulerBreakerReleaseBatch != 33 {
		t.Fatalf("SchedulerBreakerReleaseBatch got %d want 33", rc.SchedulerBreakerReleaseBatch)
	}
	if rc.SchedulerBreakerFloorPct != 70 {
		t.Fatalf("SchedulerBreakerFloorPct got %d want 70", rc.SchedulerBreakerFloorPct)
	}
	if rc.SchedulerPortDownReleaseBatch != 40 {
		t.Fatalf("SchedulerPortDownReleaseBatch got %d want 40", rc.SchedulerPortDownReleaseBatch)
	}
}

func TestAdaptiveSchedulerLiveFeedbackIncreasesStoreWhenHealthy(t *testing.T) {
	rc := robotconfig.RuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
	applyAdaptiveSchedulerConfig(&rc, adaptiveSchedulerSignals{
		Live:          true,
		Running:       590,
		Connecting:    3,
		StoreRunning:  5,
		Actors:        600,
		Idle:          20,
		GamePortReady: true,
	})
	if rc.SchedulerStoreConcurrent != 55 {
		t.Fatalf("SchedulerStoreConcurrent got %d want 55", rc.SchedulerStoreConcurrent)
	}
	if rc.AutoStoreProbabilityPercent != 26 {
		t.Fatalf("AutoStoreProbabilityPercent got %d want 26", rc.AutoStoreProbabilityPercent)
	}
	if rc.AutoMoveIntervalMinSec != 9 || rc.AutoMoveIntervalMaxSec != 29 {
		t.Fatalf("AutoMoveInterval got %d..%d want 9..29", rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec)
	}
	if rc.AutoShoutIntervalMinSec != 58 || rc.AutoShoutIntervalMaxSec != 166 {
		t.Fatalf("AutoShoutInterval got %d..%d want 58..166", rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
	}
	if rc.AutoStoreIntervalMinSec != 60 || rc.AutoStoreIntervalMaxSec != 156 {
		t.Fatalf("AutoStoreInterval got %d..%d want 60..156", rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
	}
}

func TestAdaptiveSchedulerLiveFeedbackReducesPressure(t *testing.T) {
	rc := robotconfig.RuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
	applyAdaptiveSchedulerConfig(&rc, adaptiveSchedulerSignals{
		Live:          true,
		Running:       420,
		Connecting:    120,
		StoreRunning:  28,
		Actors:        600,
		Idle:          0,
		GamePortReady: true,
	})
	if rc.SchedulerOnlineBatchSize != 30 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 30", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 7 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 7", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerStoreConcurrent != 15 {
		t.Fatalf("SchedulerStoreConcurrent got %d want 15", rc.SchedulerStoreConcurrent)
	}
	if rc.AutoStoreProbabilityPercent != 5 {
		t.Fatalf("AutoStoreProbabilityPercent got %d want 5", rc.AutoStoreProbabilityPercent)
	}
	if rc.AutoMoveIntervalMinSec != 16 || rc.AutoMoveIntervalMaxSec != 49 {
		t.Fatalf("AutoMoveInterval got %d..%d want 16..49", rc.AutoMoveIntervalMinSec, rc.AutoMoveIntervalMaxSec)
	}
	if rc.AutoShoutIntervalMinSec != 97 || rc.AutoShoutIntervalMaxSec != 277 {
		t.Fatalf("AutoShoutInterval got %d..%d want 97..277", rc.AutoShoutIntervalMinSec, rc.AutoShoutIntervalMaxSec)
	}
	if rc.AutoStoreIntervalMinSec != 112 || rc.AutoStoreIntervalMaxSec != 292 {
		t.Fatalf("AutoStoreInterval got %d..%d want 112..292", rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
	}
}

func TestAdaptiveSchedulerLiveFeedbackSpeedsFillWhenConnectionRoom(t *testing.T) {
	rc := robotconfig.RuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
	applyAdaptiveSchedulerConfig(&rc, adaptiveSchedulerSignals{
		Live:          true,
		Running:       550,
		Connecting:    5,
		StoreRunning:  25,
		Actors:        570,
		Idle:          0,
		GamePortReady: true,
	})
	if rc.SchedulerOnlineStartRate != 21 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 21", rc.SchedulerOnlineStartRate)
	}
}

func TestAdaptiveSchedulerMissingTargetFillsInsteadOfPressure(t *testing.T) {
	rc := robotconfig.RuntimeConfig{AutoTargetOnlineCount: 20, MaxOnlineRobots: 1000}
	decision := applyAdaptiveSchedulerConfig(&rc, adaptiveSchedulerSignals{
		Live:          true,
		Running:       14,
		Connecting:    0,
		Actors:        14,
		Idle:          0,
		GamePortReady: true,
	})
	if decision.Mode != schedulerPolicyFill {
		t.Fatalf("mode got %s want %s reason=%s", decision.Mode, schedulerPolicyFill, decision.Reason)
	}
	if rc.SchedulerOnlineBatchSize <= 0 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want positive fill batch", rc.SchedulerOnlineBatchSize)
	}
}

func TestAdaptiveSchedulerFreezesScaleUpOnPendingBacklog(t *testing.T) {
	rc := robotconfig.RuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
	decision := applyAdaptiveSchedulerConfig(&rc, adaptiveSchedulerSignals{
		Live:          true,
		Running:       130,
		Connecting:    0,
		Actors:        420,
		Idle:          290,
		GamePortReady: true,
	})
	if decision.Mode != schedulerPolicyPressure {
		t.Fatalf("mode got %s want %s reason=%s", decision.Mode, schedulerPolicyPressure, decision.Reason)
	}
	if rc.SchedulerOnlineBatchSize != -1 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want frozen -1", rc.SchedulerOnlineBatchSize)
	}
	if got := robotconfig.ScaleUpBatch(rc); got != 0 {
		t.Fatalf("schedulerScaleUpBatch got %d want 0", got)
	}
}

func TestLoadRobotConfigPreservesAdaptiveScaleFreeze(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[auto]\nauto_target_online_count = 600\n")
	m.autoMu.Lock()
	m.autoStats.UpdatedAt = time.Now()
	m.autoStats.Running = 130
	m.autoStats.Idle = 150
	m.autoStats.Actors = 700
	m.autoStats.GamePortReady = true
	m.autoMu.Unlock()
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != -1 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want adaptive freeze -1", rc.SchedulerOnlineBatchSize)
	}
	if got := robotconfig.ScaleUpBatch(rc); got != 0 {
		t.Fatalf("schedulerScaleUpBatch got %d want 0", got)
	}
}
