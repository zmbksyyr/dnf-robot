package robotconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadFileMapsRuntimeSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "robot_config.ini")
	raw := `[create]
level_min = 61
level_max = 72
jobs = 1, 2 2 invalid 3
grow_types = 4,5
robot_uid_start = 18000000
robot_uid_end = 18000999
robot_uid_guard = 18999999
name_ascii_fallback = enabled
name_ascii_prefix = testbot
default_money = 222
default_coin = 7
inventory_capacity = 24

[spawn]
spawn_fixed = on
spawn_village = 2
spawn_fallback_village = 3
spawn_area = 4
spawn_x_min = 10
spawn_x_max = 20
spawn_y_min = 30
spawn_y_max = 40

[move]
move_speed_min = 101
move_speed_max = 202
move_type = 6
move_steps = 7
move_step_delay_ms = 800

[online]
login_delay_ms = 1500
reconnect_delay_ms = 6000
max_reconnect = 4
max_online_robots = 321
max_online_per_command = 123
online_dispatch_interval_ms = 250
online_confirm_timeout_ms = 12000

[equipment]
equip_slots = 1,3,3,5
equip_rarity_min = 1
equip_rarity_max = 4
prefer_equip_sets = off
equip_set_min_slots = 3

[avatar]
avatar_slots = 0,2,4
min_avatar_slots = 3
prefer_avatar_sets = no
avatar_set_min_slots = 3

[store]
store_item_allow_ids = 11,12
store_item_deny_ids = 21 22
store_item_slots = 2
store_item_count_min = 3
store_item_count_max = 5
store_price_min = 1000
store_price_max = 2000
store_inventory_start_box_index = 8
store_confirm_timeout_sec = 31

[follow]
follow_account = leader
follow_radius_x = 88
follow_radius_y = 44

[shout]
shout_delay_ms = 333
shout_send_enabled = disabled

[auto]
auto_actions = false
auto_target_online_count = 200
auto_move_interval_min_sec = 7
auto_move_interval_max_sec = 14
auto_game_port_stable_sec = 16
auto_game_port_check_timeout_ms = 900

[scheduler]
bad_recover_sec = 70
bad_failures = 4
metrics_interval_sec = 12
store_concurrent = 15
online_batch_size = 42
online_start_rate = 17
online_fill_timeout_sec = 99
breaker_abnormal_percent = 25
breaker_pause_sec = 80
breaker_release_batch = 13
breaker_floor_percent = 60
port_down_release_batch = 11

[system]
actor_poll_ms = 777
manual_action_timeout_sec = 90
packet_rate_per_sec = 30
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rc.LevelMin != 61 || rc.LevelMax != 72 || rc.RobotUIDStart != 18000000 || rc.RobotUIDEnd != 18000999 || rc.RobotUIDGuard != 18999999 {
		t.Fatalf("create config not loaded: %+v", rc)
	}
	if !reflect.DeepEqual(rc.Jobs, []int{1, 2, 3}) || !reflect.DeepEqual(rc.GrowTypes, []int{4, 5}) {
		t.Fatalf("integer lists not loaded: jobs=%v grow_types=%v", rc.Jobs, rc.GrowTypes)
	}
	if !rc.NameASCIIFallback || !rc.SpawnFixed || rc.PreferEquipSets || rc.PreferAvatarSets || rc.ShoutSendEnabled || rc.AutoActions {
		t.Fatalf("boolean aliases not loaded: %+v", rc)
	}
	if rc.SpawnVillage != 2 || rc.MoveSteps != 7 || rc.MaxOnlineRobots != 321 || rc.MaxOnlinePerCommand != 123 {
		t.Fatalf("runtime sections not loaded: %+v", rc)
	}
	if !reflect.DeepEqual(rc.EquipSlots, []int{1, 3, 5}) || !reflect.DeepEqual(rc.AvatarSlots, []int{0, 2, 4}) {
		t.Fatalf("slot lists not loaded: equipment=%v avatar=%v", rc.EquipSlots, rc.AvatarSlots)
	}
	if !reflect.DeepEqual(rc.StoreItemAllowIDs, []int{11, 12}) || !reflect.DeepEqual(rc.StoreItemDenyIDs, []int{21, 22}) {
		t.Fatalf("store lists not loaded: allow=%v deny=%v", rc.StoreItemAllowIDs, rc.StoreItemDenyIDs)
	}
	if rc.FollowAccount != "leader" || rc.AutoTargetOnlineCount != 200 || rc.SchedulerOnlineBatchSize != 42 || rc.SystemActorPollMS != 777 {
		t.Fatalf("follow/auto/scheduler/system config not loaded: %+v", rc)
	}
}

func TestLoadFileKeepsDefaultsForMissingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "robot_config.ini")
	if err := os.WriteFile(path, []byte("[create]\nlevel_min = 60\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Default()
	if rc.LevelMin != 60 || rc.LevelMax != want.LevelMax || rc.RobotUIDStart != want.RobotUIDStart || rc.RobotUIDEnd != 17000999 || rc.RobotUIDGuard != 17999999 || rc.MinAvatarSlots != 8 {
		t.Fatalf("defaults not preserved: %+v", rc)
	}
}

func TestLoadFileReturnsReadError(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.ini")); err == nil {
		t.Fatal("LoadFile() error = nil, want read error")
	}
}
