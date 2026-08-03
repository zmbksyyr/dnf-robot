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

func TestWorldHornCacheBoundsSuccessfulEntries(t *testing.T) {
	cache := NewWorldHornCache()
	for cid := 1; cid <= maxWorldHornCacheEntries+100; cid++ {
		if err := cache.Ensure(cid, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	cache.access.Lock()
	got := len(cache.entries)
	cache.access.Unlock()
	if got != maxWorldHornCacheEntries {
		t.Fatalf("cache entries = %d, want %d", got, maxWorldHornCacheEntries)
	}
}

func TestWorldHornCachePanicDoesNotPoisonEntry(t *testing.T) {
	cache := NewWorldHornCache()
	if err := cache.Ensure(101, func() error { panic("bad verifier") }); err == nil {
		t.Fatal("panicking verifier returned nil error")
	}
	calls := 0
	if err := cache.Ensure(101, func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("verification calls = %d, want retry after panic", calls)
	}
}

func TestWorldHornCacheContainsPanicWhenCapacityIsFullyInFlight(t *testing.T) {
	cache := NewWorldHornCache()
	for cid := 1; cid <= maxWorldHornCacheEntries; cid++ {
		cache.entries[cid] = &worldHornCacheEntry{done: make(chan struct{})}
	}
	if err := cache.Ensure(maxWorldHornCacheEntries+1, func() error {
		panic("capacity verifier")
	}); err == nil {
		t.Fatal("capacity bypass let verifier panic escape as nil")
	}
}
