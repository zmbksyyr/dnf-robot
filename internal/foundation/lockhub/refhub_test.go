package lockhub

import "testing"

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
