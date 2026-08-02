package webadmin

import (
	"sync"
	"testing"
	"time"
)

func TestBackgroundSupervisorStepRecoversPanic(t *testing.T) {
	fallback := 5 * time.Second
	if got := runBackgroundSupervisorStep("TEST_SUPERVISOR", fallback, func() time.Duration {
		panic("broken reconcile")
	}); got != fallback {
		t.Fatalf("panic delay = %s, want %s", got, fallback)
	}

	want := 2 * time.Second
	if got := runBackgroundSupervisorStep("TEST_SUPERVISOR", fallback, func() time.Duration { return want }); got != want {
		t.Fatalf("next step delay = %s, want %s", got, want)
	}
}

func TestBackgroundSupervisorStopIsConcurrentAndIdempotent(t *testing.T) {
	stop := make(chan struct{})
	done := make(chan struct{})
	close(done)
	shutdown := stopBackgroundSupervisor(stop, done)

	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			shutdown()
		}()
	}
	workers.Wait()

	select {
	case <-stop:
	default:
		t.Fatal("supervisor stop channel was not closed")
	}
}
