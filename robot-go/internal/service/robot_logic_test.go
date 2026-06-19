package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"robot/internal/config"
)

func TestRobotStateName(t *testing.T) {
	tests := map[int]string{
		0:  "stop",
		1:  "init",
		2:  "login",
		3:  "running",
		4:  "clean",
		5:  "wrong",
		99: "unknown",
	}
	for state, want := range tests {
		if got := robotStateName(state); got != want {
			t.Fatalf("state %d: got %q want %q", state, got, want)
		}
	}
}

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

func TestSummarizeRuntimeStatus(t *testing.T) {
	running, connecting, stores := summarizeRuntimeStatusSlice([]RuntimeRobotStatus{
		{StateName: "running", DisconnectReason: 0},
		{StateName: "running", DisconnectReason: 0, RobotType: 2, StoreDisplayAck: true},
		{StateName: "running", DisconnectReason: 1, RobotType: 2, StoreDisplayAck: true},
		{StateName: "init", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 0},
		{StateName: "login", DisconnectReason: 2},
		{StateName: "stop", DisconnectReason: 0},
	})
	if running != 2 {
		t.Fatalf("running got %d want 2", running)
	}
	if connecting != 2 {
		t.Fatalf("connecting got %d want 2", connecting)
	}
	if stores != 1 {
		t.Fatalf("stores got %d want 1", stores)
	}
}

func TestActiveRuntimeStatusRequiresNoDisconnect(t *testing.T) {
	tests := []struct {
		name string
		st   RuntimeRobotStatus
		want bool
	}{
		{name: "running", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 0}, want: true},
		{name: "running disconnected", st: RuntimeRobotStatus{StateName: "running", DisconnectReason: 8}, want: false},
		{name: "login", st: RuntimeRobotStatus{StateName: "login", DisconnectReason: 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activeRuntimeStatus(tt.st); got != tt.want {
				t.Fatalf("activeRuntimeStatus() got %v want %v", got, tt.want)
			}
		})
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

func TestEquipmentTypeRecognizesTitleAndMagicStone(t *testing.T) {
	if got := equipmentType("[title name]"); got != 2 {
		t.Fatalf("title name type got %d want 2", got)
	}
	if got := equipmentType("[magic stone]"); got != 12 {
		t.Fatalf("magic stone type got %d want 12", got)
	}
}

func TestParseJobsRecognizesMultiWordJobs(t *testing.T) {
	got := parseJobs("`[swordman]`\t\n`[at gunner]`\n`[thief]`\n`[at fighter]`\n`[at mage]`\n`[demonic swordman]`")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})

	got = parseJobs("swordman at gunner thief at fighter at mage demonic swordman")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})
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

func TestPrepareShoutSeparatesLocalAndWorld(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")

	localType, localChannel, localOut := m.prepareShout(shoutTemplates{}, "hello", false)
	if localType != 3 || localChannel != "local" || localOut != "hello" {
		t.Fatalf("local shout got type=%d channel=%s out=%q", localType, localChannel, localOut)
	}

	worldType, worldChannel, worldOut := m.prepareShout(shoutTemplates{}, "hello", true)
	if worldType != 80 || worldChannel != "world" {
		t.Fatalf("world shout got type=%d channel=%s, want type=80 channel=world", worldType, worldChannel)
	}
	if !strings.Contains(worldOut, "服务器喇叭()") {
		t.Fatalf("world shout missing server horn marker: %q", worldOut)
	}
}

func TestWriteEquipSlotUsesHighIntensify(t *testing.T) {
	rc := robotRuntimeConfig{EquipIntensifyMin: 0, EquipIntensifyMax: 10}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		writeEquipSlot(raw, equipmentCatalogItem{ID: 1000 + i, ItemType: 3}, rng, rc)
		if raw[6] < 7 || raw[6] > 10 {
			t.Fatalf("armor intensify got %d want 7..10", raw[6])
		}
	}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		writeEquipSlot(raw, equipmentCatalogItem{ID: 2000 + i, ItemType: 1}, rng, rc)
		if raw[6] < 8 || raw[6] > 15 {
			t.Fatalf("weapon intensify got %d want 8..15", raw[6])
		}
	}
}

func testRobotManagerWithConfig(t *testing.T, robotConfig string) *RobotManager {
	t.Helper()
	configDir := t.TempDir()
	if robotConfig != "" {
		if err := os.WriteFile(filepath.Join(configDir, "robot_config.ini"), []byte(robotConfig), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewRobotManager(nil, &config.SysConfig{ConfigDir: configDir}, nil)
}

func assertIntSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length got %d want %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] got %d want %d: got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestSupervisorMaintainsAutoActorSlots(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	s.ensureAutoActorSlots(robotRuntimeConfig{SchedulerOnlineBatchSize: 3}, 3)
	if got := s.actorCounts(time.Now(), robotRuntimeConfig{}).auto; got != 3 {
		t.Fatalf("active actors got %d want 3", got)
	}
	s.ensureAutoActorSlots(robotRuntimeConfig{SchedulerOnlineBatchSize: 3}, 1)
	if got := s.actorCounts(time.Now(), robotRuntimeConfig{}).auto; got != 1 {
		t.Fatalf("active actors after shrink got %d want 1", got)
	}
	s.stopAll(false)
}

func TestSupervisorLeaseUIDSkipsDuplicatesAndBlocked(t *testing.T) {
	s := NewRobotSupervisor(nil, nil)
	a := &robotActor{slotID: 1}
	if !s.tryLeaseUID(101, a) {
		t.Fatalf("first lease should succeed")
	}
	if s.tryLeaseUID(101, &robotActor{slotID: 2}) {
		t.Fatalf("duplicate lease should fail")
	}
	s.unleaseUID(101, a)
	s.blockedUID[101] = struct{}{}
	if s.tryLeaseUID(101, a) {
		t.Fatalf("blocked uid should not lease")
	}
}

func TestSupervisorStopUIDsRemovesActorsAndBlocked(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	a1 := newRobotActor(1, robotActorAuto, s.runtime)
	a2 := newRobotActor(2, robotActorAuto, s.runtime)
	a1.start()
	a2.start()
	s.actors[1] = a1
	s.actors[2] = a2
	s.uidActors[101] = a1
	s.uidActors[102] = a2
	s.blockedUID[101] = struct{}{}

	if got := s.StopUIDs([]int{101, 102, 102}, false); got != 2 {
		t.Fatalf("StopUIDs got %d want 2", got)
	}
	if len(s.actors) != 0 || len(s.uidActors) != 0 {
		t.Fatalf("StopUIDs should remove actors and leases, actors=%d leases=%d", len(s.actors), len(s.uidActors))
	}
	if _, ok := s.blockedUID[101]; ok {
		t.Fatalf("StopUIDs should clear blocked marker for removed uid")
	}
}

func TestRobotActorOfflineKeepsUIDAttached(t *testing.T) {
	a := &robotActor{slotID: 1, mode: robotActorAuto}
	a.resetForUID(101)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired || snap.State != robotActorAssigned {
		t.Fatalf("assigned snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.setOnlineDesired(false)
	a.tick(time.Now())
	if snap := a.snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.State != robotActorOffline {
		t.Fatalf("offline snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}
	a.setOnlineDesired(true)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired {
		t.Fatalf("online desired should re-open without detaching uid, uid=%d desired=%v", snap.UID, snap.OnlineDesired)
	}
}

func TestSupervisorAttachUIDUsesEmptyActorSlot(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	s := NewRobotSupervisor(m, NewRobotRuntime(m))
	a := newRobotActor(1, robotActorAuto, s.runtime)
	a.start()
	defer a.stopAndWait(time.Second)
	s.actors[1] = a

	if !s.AttachUID(101, time.Second) {
		t.Fatalf("AttachUID should use empty actor")
	}
	if !s.HasUID(101) {
		t.Fatalf("attached uid should be leased")
	}
	if snap := a.snapshot(); snap.UID != 101 || snap.State != robotActorAssigned {
		t.Fatalf("actor snapshot uid=%d state=%s", snap.UID, snap.State)
	}
	if s.AttachUID(102, time.Second) {
		t.Fatalf("AttachUID should fail when no empty actor remains")
	}
}

func TestActorStatusFieldsDeriveLedgerState(t *testing.T) {
	actor := robotActorSnapshot{State: robotActorOffline, OnlineDesired: false}
	if got := actorOperation(actor); got != "offline" {
		t.Fatalf("offline operation got %q", got)
	}
	actor = robotActorSnapshot{State: robotActorBusy, BusyKind: "store", OnlineDesired: true}
	if got := actorOperation(actor); got != "store" {
		t.Fatalf("busy operation got %q", got)
	}
	if got := actorHealthState("ok", robotActorSnapshot{Failures: 1}); got != "suspect" {
		t.Fatalf("health got %q want suspect", got)
	}
}

func TestRobotActorSnapshotHelpers(t *testing.T) {
	cases := []struct {
		name    string
		snap    robotActorSnapshot
		pending bool
		empty   bool
	}{
		{name: "empty", snap: robotActorSnapshot{}, pending: true, empty: true},
		{name: "offline attached", snap: robotActorSnapshot{UID: 1, State: robotActorOffline}, pending: true},
		{name: "online pending", snap: robotActorSnapshot{UID: 1, State: robotActorOnline}, pending: true},
		{name: "running", snap: robotActorSnapshot{UID: 1, State: robotActorRunning}, pending: false},
		{name: "busy", snap: robotActorSnapshot{UID: 1, State: robotActorBusy}, pending: false},
	}
	for _, tc := range cases {
		if got := tc.snap.schedulerPending(); got != tc.pending {
			t.Fatalf("%s pending got %v want %v", tc.name, got, tc.pending)
		}
		if got := tc.snap.empty(); got != tc.empty {
			t.Fatalf("%s empty got %v want %v", tc.name, got, tc.empty)
		}
	}
}

func TestStructuralOperationState(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	done := m.beginStructuralOp("cleanup")
	op, started, active := m.structuralOperation()
	if !active || op != "cleanup" || started.IsZero() {
		t.Fatalf("structural op got active=%v op=%q started=%s", active, op, started)
	}
	done()
	if op, _, active := m.structuralOperation(); active || op != "" {
		t.Fatalf("structural op should clear, active=%v op=%q", active, op)
	}
}

func TestRobotActorUnhealthyByFailureCount(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 801, failures: 5, firstFailureAt: now.Add(-10 * time.Second)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}
	a.busy = true
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 5, SchedulerBadRecoverSec: 300})
	if status.RecycleUID || status.Health != robotActorHealthBusy {
		t.Fatalf("busy actor status got recycle=%v health=%s, want busy without recycle", status.RecycleUID, status.Health)
	}
}

func TestSelectStoreItemsUsesAllowDenyAndMaterialRules(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 3037, Level: 1, Slot: "material", Trade: true, BasicMaterial: true, Icon: "stackable/material.img", FieldImage: "material/ore", StackLimit: 1000},
		{ID: 3031, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/cloth", StackLimit: 1000},
		{ID: 3032, Level: 99, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/high", StackLimit: 1000},
		{ID: 7312, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/deny", StackLimit: 1000},
		{ID: 3034, Level: 1, Slot: "material", Trade: true, Icon: "stackable/etc.img", FieldImage: "material/bad_icon", StackLimit: 1000},
		{ID: 3035, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 105,
		StoreItemAllowIDs:      []int{3037, 3031, 3032, 3034, 3035, 7312},
		StoreItemDenyIDs:       []int{7312},
	})

	got := storeItemIDSet(items)
	if len(got) != 1 || !got[3037] {
		t.Fatalf("selected IDs got %v want only basic allowed material 3037", got)
	}
}

func TestSelectStoreItemsFallbacksToAllowIDs(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 9001, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/not_allowed", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 105,
		StoreItemAllowIDs:      []int{3037, 3031},
		StoreItemDenyIDs:       []int{3031},
	})

	if len(items) != 1 || items[0].ID != 3037 || items[0].Slot != "material" {
		t.Fatalf("fallback items got %+v want synthetic material 3037", items)
	}
}

func TestStorePointCoordinatorCachesSourceMD5(t *testing.T) {
	configDir := t.TempDir()
	data := writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 80, Use: true}})
	c := newStorePointCoordinator(configDir)
	if len(c.points) == 0 {
		t.Fatalf("expected generated store points")
	}
	cacheData, err := os.ReadFile(filepath.Join(configDir, storePointCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	var cache storePointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	if cache.SourceMD5 != hex.EncodeToString(sum[:]) {
		t.Fatalf("cache md5 got %q want source md5", cache.SourceMD5)
	}
}

func TestStorePointCoordinatorDoesNotReuseFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, false, "test_failed")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("failed point was reused after restart: %s", next.PointID)
	}
}

func TestStorePointCoordinatorExploresUnknownBeforeReusingSuccess(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 160, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "test_success")
	second, ok := c.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if second.PointID == first.PointID {
		t.Fatalf("success point reused before unknown points were tried: %s", second.PointID)
	}
}

func TestRobotActorBadByFailureCount(t *testing.T) {
	a := &robotActor{mode: robotActorAuto, uid: 1001, failures: 3}
	status := a.status(time.Now(), robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_count" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_count recycle", status.RecycleUID, status.HealthReason)
	}

	a.failures = 2
	status = a.status(time.Now(), robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle below failure threshold")
	}
}

func TestRobotActorBadByRecoveryWindow(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, failures: 1, firstFailureAt: now.Add(-61 * time.Second)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_window" {
		t.Fatalf("actor status got recycle=%v reason=%q, want failure_window recycle", status.RecycleUID, status.HealthReason)
	}

	a.firstFailureAt = now.Add(-59 * time.Second)
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle before recovery window expires")
	}

	a.failures = 0
	a.firstFailureAt = now.Add(-61 * time.Second)
	status = a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if status.RecycleUID {
		t.Fatalf("actor should not recycle by failure window without failures, reason=%q", status.HealthReason)
	}
}

func TestRobotActorPendingOnlineUsesConfirmTimeout(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, state: robotActorOnline, lastOnlineTry: now.Add(-61 * time.Second)}
	a.markOnlinePending(now.Add(-61 * time.Second))
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if !status.RecycleUID || status.HealthReason != "online_confirm_timeout" {
		t.Fatalf("pending actor status got recycle=%v reason=%q, want online_confirm_timeout recycle", status.RecycleUID, status.HealthReason)
	}
}

func TestRobotActorHealthyClearsOnlineConfirmTimer(t *testing.T) {
	a := &robotActor{mode: robotActorAuto, uid: 1001, lastOnlineTry: time.Now().Add(-2 * time.Minute), firstFailureAt: time.Now().Add(-2 * time.Minute)}
	a.markOnlineHealthy()
	s := a.snapshot()
	if !s.LastOnlineTry.IsZero() || !s.FirstFailureAt.IsZero() {
		t.Fatalf("healthy actor should clear online timers, got last=%s first_failure=%s", s.LastOnlineTry, s.FirstFailureAt)
	}
}

func TestRobotActorStoreFailedCooldownDoesNotPoisonHealth(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, lastOnlineTry: now.Add(-2 * time.Minute), firstFailureAt: now.Add(-2 * time.Minute), failures: 1}
	a.markOnlineHealthy()
	a.nextStore = now.Add(60 * time.Second)
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID {
		t.Fatalf("store failure cooldown should not recycle actor, reason=%q", status.HealthReason)
	}
	if !status.LastOnlineTry.IsZero() || !status.FirstFailureAt.IsZero() || status.Failures != 0 {
		t.Fatalf("store failure cooldown should clear health timers, got last=%s first=%s failures=%d", status.LastOnlineTry, status.FirstFailureAt, status.Failures)
	}
}

func TestRobotActorOnlineAttemptWithoutPendingDoesNotRecycle(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001, state: robotActorOnline, lastOnlineTry: now.Add(-2 * time.Minute)}
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60, OnlineConfirmTimeoutMS: 60000})
	if status.RecycleUID {
		t.Fatalf("online attempt without pending marker should not recycle, reason=%q", status.HealthReason)
	}
}

func testRobotManagerWithStackableCatalog(t *testing.T, catalog []equipmentCatalogItem) *RobotManager {
	t.Helper()
	configDir := t.TempDir()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "pvf_stackable_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return NewRobotManager(nil, &config.SysConfig{ConfigDir: configDir}, nil)
}

func storeItemIDSet(items []equipmentCatalogItem) map[int]bool {
	out := make(map[int]bool, len(items))
	for _, item := range items {
		out[item.ID] = true
	}
	return out
}

func writeStoreMapCatalog(t *testing.T, configDir string, maps []mapCatalogItem) []byte {
	t.Helper()
	data, err := json.Marshal(maps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "pvf_map_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}
