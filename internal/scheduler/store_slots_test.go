package scheduler

import (
	"testing"

	robotconfig "robot/internal/capability/robotconfig"
)

func TestAutoStoreSlotLimitChangePreservesActiveCount(t *testing.T) {
	manager := &RobotManager{}
	limitTwo := robotconfig.RuntimeConfig{SchedulerStoreConcurrent: 2}

	releaseFirst, ok := manager.acquireAutoStoreSlot(limitTwo)
	if !ok {
		t.Fatal("first slot was not acquired")
	}
	releaseSecond, ok := manager.acquireAutoStoreSlot(limitTwo)
	if !ok {
		t.Fatal("second slot was not acquired")
	}
	if _, ok := manager.acquireAutoStoreSlot(limitTwo); ok {
		t.Fatal("acquired a third slot at limit two")
	}

	limitOne := robotconfig.RuntimeConfig{SchedulerStoreConcurrent: 1}
	if _, ok := manager.acquireAutoStoreSlot(limitOne); ok {
		t.Fatal("lowering the limit reset active occupancy")
	}
	releaseFirst()
	if _, ok := manager.acquireAutoStoreSlot(limitOne); ok {
		t.Fatal("one active operation must still fill the lowered limit")
	}
	releaseSecond()

	releaseThird, ok := manager.acquireAutoStoreSlot(limitOne)
	if !ok {
		t.Fatal("slot did not reopen after all prior operations released")
	}
	releaseThird()
	releaseThird()
	if manager.autoStoreActive != 0 {
		t.Fatalf("active slots = %d after idempotent release, want 0", manager.autoStoreActive)
	}
}

func TestAutoStoreSlotLimitIncreaseAddsOnlyAvailableCapacity(t *testing.T) {
	manager := &RobotManager{}
	releaseFirst, ok := manager.acquireAutoStoreSlot(robotconfig.RuntimeConfig{SchedulerStoreConcurrent: 1})
	if !ok {
		t.Fatal("first slot was not acquired")
	}

	limitThree := robotconfig.RuntimeConfig{SchedulerStoreConcurrent: 3}
	releaseSecond, ok := manager.acquireAutoStoreSlot(limitThree)
	if !ok {
		t.Fatal("second slot was not acquired after raising limit")
	}
	releaseThird, ok := manager.acquireAutoStoreSlot(limitThree)
	if !ok {
		t.Fatal("third slot was not acquired after raising limit")
	}
	if _, ok := manager.acquireAutoStoreSlot(limitThree); ok {
		t.Fatal("raising the limit reset the existing active slot")
	}

	releaseFirst()
	releaseSecond()
	releaseThird()
}
