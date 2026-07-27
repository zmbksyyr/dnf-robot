package lockhub

import (
	"sync"
	"testing"
)

func TestWithRobotSerializesSameUID(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	value := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.WithRobot(1, "test", func() error {
				value++
				return nil
			}); err != nil {
				t.Errorf("WithRobot error: %v", err)
			}
		}()
	}
	wg.Wait()
	if value != 100 {
		t.Fatalf("value=%d, want 100", value)
	}
}

func TestWithResourceTracksResourceLock(t *testing.T) {
	h := New()
	if err := h.WithResource("store", "slot-1", "test", func() error {
		return nil
	}); err != nil {
		t.Fatalf("WithResource error: %v", err)
	}
	_, resources := h.ActiveLocks()
	if resources != 1 {
		t.Fatalf("resources=%d, want 1", resources)
	}
}
