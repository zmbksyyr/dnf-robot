package scheduler

import (
	"testing"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
)

func TestDisabledAutoGuardDoesNotWaitForManualMutationWithoutAutoActors(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "manager-disabled", true: "config-disabled"}[enabled], func(t *testing.T) {
			manager := testRobotManagerWithConfig(t, "")
			manager.autoEnabled = enabled
			supervisor := NewRobotSupervisor(manager, actorTestRuntime{})
			t.Cleanup(supervisor.stopAll)
			registry := newSupervisorActorRegistry(supervisor)
			if !registry.AttachUID(101, time.Second) {
				t.Fatal("manual actor was not attached")
			}
			now := time.Now()
			supervisor.nextMetrics = now.Add(time.Hour)
			manager.actorMutationMu.Lock()
			done := make(chan bool, 1)
			go func() {
				done <- supervisor.handleAutoGuards(now, robotconfig.RuntimeConfig{AutoActions: !enabled}, adaptiveSchedulerSignals{})
			}()
			select {
			case guarded := <-done:
				manager.actorMutationMu.Unlock()
				if !guarded {
					t.Fatal("disabled automation was not guarded")
				}
			case <-time.After(200 * time.Millisecond):
				manager.actorMutationMu.Unlock()
				<-done
				t.Fatal("empty auto cleanup waited for an unrelated manual operation")
			}
			if !registry.HasUID(101) {
				t.Fatal("disabled auto guard removed the manual actor")
			}
		})
	}
}

func TestDisabledAutoGuardDrainsAutoActorsAndFindsLaterArrivals(t *testing.T) {
	manager := testRobotManagerWithConfig(t, "")
	manager.autoEnabled = false
	supervisor := NewRobotSupervisor(manager, actorTestRuntime{})
	t.Cleanup(supervisor.stopAll)
	registry := newSupervisorActorRegistry(supervisor)
	if !registry.AttachUID(101, time.Second) {
		t.Fatal("manual actor was not attached")
	}
	now := time.Now()
	supervisor.nextMetrics = now.Add(time.Hour)
	rc := robotconfig.RuntimeConfig{SchedulerOnlineBatchSize: 2}
	for round := 0; round < 2; round++ {
		supervisor.ensureAutoActorSlots(rc, 2)
		if got := supervisor.actorCounts(now, rc).Auto; got != 2 {
			t.Fatalf("round %d: expected two auto actors, got %d", round, got)
		}
		if !supervisor.handleAutoGuards(now, rc, adaptiveSchedulerSignals{}) {
			t.Fatal("disabled automation was not guarded")
		}
		if got := supervisor.actorCounts(now, rc).Auto; got != 0 {
			t.Fatalf("round %d: auto actors survived disabled guard: %d", round, got)
		}
		if !registry.HasUID(101) {
			t.Fatal("draining auto actors removed the manual actor")
		}
	}
}
