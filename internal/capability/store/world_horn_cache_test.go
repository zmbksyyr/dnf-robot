package store

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWorldHornCacheCachesSuccessAndRetriesFailure(t *testing.T) {
	cache := NewWorldHornCache()
	calls := 0
	verify := func() error {
		calls++
		if calls == 1 {
			return errors.New("inventory unavailable")
		}
		return nil
	}

	if err := cache.Ensure(101, verify); err == nil {
		t.Fatal("first verification unexpectedly succeeded")
	}
	if err := cache.Ensure(101, verify); err != nil {
		t.Fatalf("verification retry: %v", err)
	}
	if err := cache.Ensure(101, verify); err != nil {
		t.Fatalf("cached verification: %v", err)
	}
	if calls != 2 {
		t.Fatalf("verification calls got %d want 2", calls)
	}

	cache.Invalidate(101)
	if err := cache.Ensure(101, verify); err != nil {
		t.Fatalf("verification after invalidation: %v", err)
	}
	if calls != 3 {
		t.Fatalf("verification calls after invalidation got %d want 3", calls)
	}
}

func TestWorldHornCacheSharesConcurrentVerification(t *testing.T) {
	cache := NewWorldHornCache()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	verify := func() error {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	}

	const callers = 32
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- cache.Ensure(101, verify)
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("shared verification: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent verification calls got %d want 1", calls.Load())
	}
}
