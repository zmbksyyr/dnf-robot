package service

import (
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

func TestSchedulerOnlineStartRate(t *testing.T) {
	tests := []struct {
		name string
		rate int
		want int
	}{
		{name: "default", rate: 0, want: 20},
		{name: "configured", rate: 8, want: 8},
		{name: "hard cap", rate: 99, want: 60},
		{name: "negative default", rate: -1, want: 20},
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

func TestSortActorsForStopKeepsSmallUIDs(t *testing.T) {
	actors := []*robotActor{
		{slotID: 1, uid: 17000001},
		{slotID: 2, uid: 17000020},
		{slotID: 3, uid: 0},
		{slotID: 4, uid: 17000005},
		{slotID: 5, uid: 17000010},
	}
	sortActorsForStop(actors)
	got := []int{actors[0].uidValue(), actors[1].uidValue(), actors[2].uidValue(), actors[3].uidValue(), actors[4].uidValue()}
	assertIntSlice(t, got, []int{0, 17000020, 17000010, 17000005, 17000001})
}

func TestLoadRobotConfigSchedulerOnlineDefaults(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 120 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 120", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 20 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 20", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 60 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 60", rc.SchedulerOnlineFillTimeout)
	}
	assertIntSlice(t, rc.EquipSlots, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	if rc.EquipIntensifyMin != 7 || rc.EquipIntensifyMax != 10 {
		t.Fatalf("EquipIntensify got %d..%d want 7..10", rc.EquipIntensifyMin, rc.EquipIntensifyMax)
	}
	if !rc.AutoActions || rc.AutoTargetOnlineCount != 20 || rc.AutoStoreProbabilityPercent != 20 {
		t.Fatalf("auto defaults got enabled=%v target=%d store_probability=%d, want true/20/20", rc.AutoActions, rc.AutoTargetOnlineCount, rc.AutoStoreProbabilityPercent)
	}
	assertIntSlice(t, rc.StoreItemAllowIDs, []int{3037, 3031, 3032, 3034, 3035})
}

func TestLoadRobotConfigSchedulerOnlineValues(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[scheduler]\nonline_batch_size = 30\nonline_start_rate = 8\nonline_fill_timeout_sec = 90\n")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 30 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 30", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 8 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 8", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 90 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 90", rc.SchedulerOnlineFillTimeout)
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

func TestLoadRobotConfigSchedulerOnlineCaps(t *testing.T) {
	m := testRobotManagerWithConfig(t, "[scheduler]\nonline_batch_size = 999\nonline_start_rate = 999\nonline_fill_timeout_sec = -1\n")
	rc := m.loadRobotConfig()
	if rc.SchedulerOnlineBatchSize != 120 {
		t.Fatalf("SchedulerOnlineBatchSize got %d want 120", rc.SchedulerOnlineBatchSize)
	}
	if rc.SchedulerOnlineStartRate != 60 {
		t.Fatalf("SchedulerOnlineStartRate got %d want 60", rc.SchedulerOnlineStartRate)
	}
	if rc.SchedulerOnlineFillTimeout != 60 {
		t.Fatalf("SchedulerOnlineFillTimeout got %d want 60", rc.SchedulerOnlineFillTimeout)
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
	s.ensureAutoActorSlots(3)
	if got := s.actorCounts(time.Now(), robotRuntimeConfig{}).auto; got != 3 {
		t.Fatalf("active actors got %d want 3", got)
	}
	s.ensureAutoActorSlots(1)
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

func TestRandomStorePositionNearUsesSpawnFixedAreas(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	for i := 0; i < 100; i++ {
		village, area, x, y := m.randomStorePositionNear(RobotInfo{Village: 3, Area: 0, X: 10, Y: 10}, robotRuntimeConfig{})
		if village != 3 || area != 0 {
			t.Fatalf("location got village=%d area=%d want 3/0", village, area)
		}
		if !inAnyStoreArea(x, y) {
			t.Fatalf("coordinate got x=%d y=%d outside fixed store areas", x, y)
		}
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
}

func TestRobotActorPendingOnlineUsesRecoveryWindow(t *testing.T) {
	now := time.Now()
	a := &robotActor{mode: robotActorAuto, uid: 1001}
	a.markOnlinePending(now.Add(-61 * time.Second))
	status := a.status(now, robotRuntimeConfig{SchedulerBadFailures: 3, SchedulerBadRecoverSec: 60})
	if !status.RecycleUID || status.HealthReason != "failure_window" {
		t.Fatalf("pending actor status got recycle=%v reason=%q, want failure_window recycle", status.RecycleUID, status.HealthReason)
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

func inAnyStoreArea(x, y int) bool {
	areas := []struct {
		xMin int
		xMax int
		yMin int
		yMax int
	}{
		{xMin: 250, xMax: 600, yMin: 180, yMax: 260},
		{xMin: 800, xMax: 1150, yMin: 220, yMax: 270},
		{xMin: 1300, xMax: 1600, yMin: 240, yMax: 320},
		{xMin: 320, xMax: 520, yMin: 390, yMax: 450},
	}
	for _, area := range areas {
		if x >= area.xMin && x <= area.xMax && y >= area.yMin && y <= area.yMax {
			return true
		}
	}
	return false
}
