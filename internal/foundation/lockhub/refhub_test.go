package lockhub

import (
	"runtime"
	"testing"
	"time"
)

func TestRefHubRemovesLockAfterRelease(t *testing.T) {
	h := NewRefHub()
	lock := h.Acquire(101)
	if !h.Active(101) {
		t.Fatalf("expected lock to be active after acquire")
	}
	h.Release(101, lock)
	if h.Active(101) {
		t.Fatalf("expected lock to be removed after release")
	}
}

func TestRefHubKeepsLockUntilAllRefsRelease(t *testing.T) {
	h := NewRefHub()
	first := h.Acquire(101)
	released := make(chan struct{})
	go func() {
		second := h.Acquire(101)
		h.Release(101, second)
		close(released)
	}()
	if !h.Active(101) {
		t.Fatalf("expected lock to stay active while first ref is held")
	}
	h.Release(101, first)
	<-released
	if h.Active(101) {
		t.Fatalf("expected lock to be removed after all refs release")
	}
}

func TestRefHubBlockedUIDDoesNotBlockOtherUIDs(t *testing.T) {
	h := NewRefHub()
	first := h.Acquire(101)

	waiting := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(waiting)
		second := h.Acquire(101)
		h.Release(101, second)
		close(done)
	}()
	<-waiting
	deadline := time.Now().Add(time.Second)
	for {
		if h.mu.TryLock() {
			refs := first.refs
			h.mu.Unlock()
			if refs == 2 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("blocked Acquire kept the RefHub mutex locked")
		}
		runtime.Gosched()
	}

	acquiredOtherUID := make(chan struct{})
	go func() {
		lock := h.Acquire(202)
		h.Release(202, lock)
		close(acquiredOtherUID)
	}()

	select {
	case <-acquiredOtherUID:
	case <-time.After(time.Second):
		t.Fatal("Acquire for an unrelated UID waited on the blocked UID")
	}

	h.Release(101, first)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked Acquire did not finish after release")
	}
}
