package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
	"robot/internal/foundation/config"
)

func TestLoadRobotConfigRefreshesExpiredSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "robot_config.ini")
	if err := os.WriteFile(path, []byte("[auto]\nauto_target_online_count = 20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewRobotManager(nil, &config.SysConfig{ConfigDir: dir}, nil)
	if got := manager.loadRobotConfig().AutoTargetOnlineCount; got != 20 {
		t.Fatalf("initial target = %d, want 20", got)
	}

	if err := os.WriteFile(path, []byte("[auto]\nauto_target_online_count = 600\n"), 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	snapshot := manager.configSnapshot.Load()
	manager.configSnapshot.Store(&robotConfigSnapshot{
		base: snapshot.base, effective: snapshot.effective, modTime: snapshot.modTime,
	})

	if got := manager.loadRobotConfig().AutoTargetOnlineCount; got != 600 {
		t.Fatalf("refreshed target = %d, want 600", got)
	}
}

func TestAdaptiveRobotConfigPublishesLivePolicy(t *testing.T) {
	manager := testRobotManagerWithConfig(t, "[auto]\nauto_target_online_count = 600\n")
	bootstrap := manager.loadRobotConfig()
	if bootstrap.SchedulerStoreConcurrent != 30 {
		t.Fatalf("bootstrap store concurrency = %d, want 30", bootstrap.SchedulerStoreConcurrent)
	}

	effective, decision := manager.refreshAdaptiveRobotConfig(adaptiveSchedulerSignals{
		Live: true, Running: 600, Actors: 600, Idle: 20, GamePortReady: true,
	})
	if decision.Mode != schedulerPolicyStore {
		t.Fatalf("policy mode = %s, want %s", decision.Mode, schedulerPolicyStore)
	}
	if effective.SchedulerStoreConcurrent <= bootstrap.SchedulerStoreConcurrent {
		t.Fatalf("effective store concurrency = %d, bootstrap = %d", effective.SchedulerStoreConcurrent, bootstrap.SchedulerStoreConcurrent)
	}
	if got := manager.loadRobotConfig().SchedulerStoreConcurrent; got != effective.SchedulerStoreConcurrent {
		t.Fatalf("published store concurrency = %d, want %d", got, effective.SchedulerStoreConcurrent)
	}
}

func TestInvalidateRobotConfigCacheRefreshesImmediately(t *testing.T) {
	manager := testRobotManagerWithConfig(t, "[auto]\nauto_target_online_count = 20\n")
	if got := manager.loadRobotConfig().AutoTargetOnlineCount; got != 20 {
		t.Fatalf("initial target = %d, want 20", got)
	}
	if _, err := manager.UpdateRobotConfig(robotcap.ConfigUpdateRequest{Updates: map[string]interface{}{
		"auto.auto_target_online_count": 600,
	}}); err != nil {
		t.Fatal(err)
	}
	if got := manager.loadRobotConfig().AutoTargetOnlineCount; got != 600 {
		t.Fatalf("updated target = %d, want 600", got)
	}
}

func BenchmarkLoadRobotConfigCached(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "robot_config.ini")
	if err := os.WriteFile(path, []byte("[auto]\nauto_target_online_count = 550\n"), 0644); err != nil {
		b.Fatal(err)
	}
	manager := NewRobotManager(nil, &config.SysConfig{ConfigDir: dir}, nil)
	_ = manager.loadRobotConfig()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.loadRobotConfig()
	}
}
