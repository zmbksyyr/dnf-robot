package service

import (
	"strings"
	"testing"
	"time"
)

func TestUpdateINITextUpdatesAndAppendsValues(t *testing.T) {
	input := "[auto]\n" +
		"auto_actions = false\n" +
		"\n" +
		"[scheduler]\n" +
		"scheduler_metrics_interval_sec = 10\n"
	out := updateINIText(input, map[string]string{
		"auto.auto_actions":                        "true",
		"scheduler.scheduler_bad_failures":         "3",
		"system.manual_action_timeout_sec":         "60",
		"scheduler.scheduler_metrics_interval_sec": "5",
	})

	for _, want := range []string{
		"auto_actions = true",
		"scheduler_metrics_interval_sec = 5",
		"scheduler_bad_failures = 3",
		"[system]\nmanual_action_timeout_sec = 60",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("updated ini missing %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("updated ini should end with newline")
	}
}

func TestPublicRobotConfigTextHidesAdaptiveKeys(t *testing.T) {
	input := "[auto]\n" +
		"auto_actions = true\n" +
		"auto_target_online_count = 600\n" +
		"auto_store_probability_percent = 100\n" +
		"auto_store_interval_min_sec = 1\n" +
		"[scheduler]\n" +
		"online_start_rate = 99\n" +
		"breaker_release_batch = 99\n"
	out := publicRobotConfigText(input)
	for _, hidden := range []string{
		"auto_store_probability_percent",
		"auto_store_interval_min_sec",
		"online_start_rate",
		"breaker_release_batch",
	} {
		if strings.Contains(out, hidden) {
			t.Fatalf("public config should hide %q:\n%s", hidden, out)
		}
	}
	for _, visible := range []string{"auto_actions = true", "auto_target_online_count = 600"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("public config missing %q:\n%s", visible, out)
		}
	}
}

func TestSchedulerOnlineStartRate(t *testing.T) {
	tests := []struct {
		name string
		rate int
		want int
	}{
		{name: "default", rate: 0, want: 20},
		{name: "configured", rate: 8, want: 8},
		{name: "hard cap", rate: 99, want: 60},
		{name: "frozen", rate: -1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schedulerOnlineStartRate(robotRuntimeConfig{SchedulerOnlineStartRate: tt.rate})
			if got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestSchedulerOnlineStartRateForNeed(t *testing.T) {
	tests := []struct {
		name    string
		need    int
		rate    int
		timeout int
		want    int
	}{
		{name: "fill 600 in 60 seconds", need: 600, rate: 20, timeout: 60, want: 20},
		{name: "small target keeps configured rate", need: 100, rate: 20, timeout: 60, want: 20},
		{name: "large target raises rate", need: 3000, rate: 20, timeout: 60, want: 50},
		{name: "hard cap", need: 6000, rate: 20, timeout: 60, want: 60},
		{name: "invalid timeout uses default", need: 600, rate: 1, timeout: 0, want: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schedulerOnlineStartRateForNeed(tt.need, robotRuntimeConfig{
				SchedulerOnlineStartRate:   tt.rate,
				SchedulerOnlineFillTimeout: tt.timeout,
			})
			if got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestSchedulerScaleBatches(t *testing.T) {
	if got := schedulerScaleUpBatch(robotRuntimeConfig{SchedulerOnlineBatchSize: 30}); got != 30 {
		t.Fatalf("scale up configured got %d want 30", got)
	}
	if got := schedulerScaleUpBatch(robotRuntimeConfig{SchedulerOnlineBatchSize: 999}); got != 120 {
		t.Fatalf("scale up cap got %d want 120", got)
	}
	if got := schedulerScaleUpBatch(robotRuntimeConfig{SchedulerOnlineBatchSize: -1}); got != 0 {
		t.Fatalf("scale up frozen got %d want 0", got)
	}
	if got := schedulerScaleDownBatch(600, 20); got != 24 {
		t.Fatalf("scale down 600->20 got %d want 24", got)
	}
	if got := schedulerScaleDownBatch(30, 20); got != 5 {
		t.Fatalf("scale down minimum got %d want 5", got)
	}
	if got := schedulerScaleDownBatch(20, 600); got != 0 {
		t.Fatalf("scale down grow path got %d want 0", got)
	}
}

func TestSchedulerCreateRoomRespectsTargetCapacity(t *testing.T) {
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 200, MaxOnlineRobots: 600}
	if got := schedulerCreateRoom(rc, 189); got != 11 {
		t.Fatalf("create room got %d want 11", got)
	}
	if got := schedulerCreateRoom(rc, 200); got != 0 {
		t.Fatalf("create room at target got %d want 0", got)
	}
	if got := schedulerCreateRoom(rc, 301); got != 0 {
		t.Fatalf("create room above target got %d want 0", got)
	}
	if got := schedulerTargetCapacity(robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 200}); got != 200 {
		t.Fatalf("target capacity got %d want max cap 200", got)
	}
}

func TestSortActorsForStopByPolicy(t *testing.T) {
	actors := []*robotActor{
		{slotID: 1, uid: 17000001},
		{slotID: 2, uid: 17000002},
		{slotID: 3, uid: 17000003},
		{slotID: 4, uid: 0},
	}
	status := map[int]RuntimeRobotStatus{
		17000001: {UID: 17000001, StateName: "running", DisconnectReason: 0},
		17000002: {UID: 17000002, StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		17000003: {UID: 17000003, StateName: "login", DisconnectReason: 0},
	}
	sortActorsForStopByPolicy(actors, status)
	got := []int{actors[0].uidValue(), actors[1].uidValue(), actors[2].uidValue(), actors[3].uidValue()}
	assertIntSlice(t, got, []int{0, 17000003, 17000002, 17000001})
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
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
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
	if rc.AutoStoreIntervalMinSec != 60 || rc.AutoStoreIntervalMaxSec != 156 {
		t.Fatalf("AutoStoreInterval got %d..%d want 60..156", rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
	}
}

func TestAdaptiveSchedulerLiveFeedbackReducesPressure(t *testing.T) {
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
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
	if rc.AutoStoreIntervalMinSec != 112 || rc.AutoStoreIntervalMaxSec != 292 {
		t.Fatalf("AutoStoreInterval got %d..%d want 112..292", rc.AutoStoreIntervalMinSec, rc.AutoStoreIntervalMaxSec)
	}
}

func TestAdaptiveSchedulerLiveFeedbackSpeedsFillWhenConnectionRoom(t *testing.T) {
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
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
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 20, MaxOnlineRobots: 1000}
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
	rc := robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000}
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
	if got := schedulerScaleUpBatch(rc); got != 0 {
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
	if got := schedulerScaleUpBatch(rc); got != 0 {
		t.Fatalf("schedulerScaleUpBatch got %d want 0", got)
	}
}

func TestBreakerActorFloorUsesSchedulerPercent(t *testing.T) {
	got := breakerActorFloor(robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 1000, SchedulerBreakerFloorPct: 70})
	if got != 420 {
		t.Fatalf("breakerActorFloor got %d want 420", got)
	}
	got = breakerActorFloor(robotRuntimeConfig{AutoTargetOnlineCount: 600, MaxOnlineRobots: 500, SchedulerBreakerFloorPct: 80})
	if got != 400 {
		t.Fatalf("breakerActorFloor capped target got %d want 400", got)
	}
	got = breakerActorFloor(robotRuntimeConfig{AutoTargetOnlineCount: 20, MaxOnlineRobots: 1000, SchedulerBreakerFloorPct: 70})
	if got != 20 {
		t.Fatalf("breakerActorFloor small target got %d want 20", got)
	}
}
