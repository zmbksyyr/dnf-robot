package dnf

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginStaticRepairCacheCachesSuccessPerDatabaseAndUID(t *testing.T) {
	var cache loginStaticRepairCache
	ctx := context.Background()
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	calls := 0
	repair := func(context.Context) bool {
		calls++
		return true
	}

	if !cache.ensure(ctx, db1, 101, repair) {
		t.Fatal("initial repair unexpectedly failed")
	}
	if !cache.ensure(ctx, db1, 101, repair) {
		t.Fatal("cached repair unexpectedly failed")
	}
	if calls != 1 {
		t.Fatalf("same database and UID repair calls got %d want 1", calls)
	}
	if !cache.ensure(ctx, db1, 102, repair) || !cache.ensure(ctx, db2, 101, repair) {
		t.Fatal("independent repair unexpectedly failed")
	}
	if calls != 3 {
		t.Fatalf("independent repair calls got %d want 3", calls)
	}
}

func TestLoginStaticRepairCacheExpiresSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	cache := loginStaticRepairCache{
		ttl: time.Minute,
		now: func() time.Time { return now },
	}
	db := &sql.DB{}
	calls := 0
	repair := func(context.Context) bool {
		calls++
		return true
	}

	if !cache.ensure(ctx, db, 101, repair) {
		t.Fatal("initial repair unexpectedly failed")
	}
	now = now.Add(time.Minute)
	if !cache.ensure(ctx, db, 101, repair) {
		t.Fatal("expired repair unexpectedly failed")
	}
	if calls != 2 {
		t.Fatalf("expired repair calls got %d want 2", calls)
	}
}

func TestLoginStaticRepairCacheRetriesFailure(t *testing.T) {
	var cache loginStaticRepairCache
	ctx := context.Background()
	db := &sql.DB{}
	calls := 0
	repair := func(context.Context) bool {
		calls++
		return calls > 1
	}

	if cache.ensure(ctx, db, 101, repair) {
		t.Fatal("first repair unexpectedly succeeded")
	}
	if !cache.ensure(ctx, db, 101, repair) {
		t.Fatal("successful retry failed")
	}
	if !cache.ensure(ctx, db, 101, repair) {
		t.Fatal("successful retry was not cached")
	}
	if calls != 2 {
		t.Fatalf("repair calls got %d want 2", calls)
	}
}

func TestLoginStaticRepairCacheInvalidatesRecreatedUID(t *testing.T) {
	var cache loginStaticRepairCache
	ctx := context.Background()
	db := &sql.DB{}
	calls := 0
	repair := func(context.Context) bool {
		calls++
		return true
	}
	if !cache.ensure(ctx, db, 101, repair) || !cache.ensure(ctx, db, 102, repair) {
		t.Fatal("initial repairs failed")
	}
	cache.invalidateUIDs([]int{101})
	if !cache.ensure(ctx, db, 101, repair) || !cache.ensure(ctx, db, 102, repair) {
		t.Fatal("repairs after invalidation failed")
	}
	if calls != 3 {
		t.Fatalf("repair calls got %d want 3", calls)
	}
}

func TestLoginStaticCacheDoesNotSkipMutableSessionRepairs(t *testing.T) {
	var cache loginStaticRepairCache
	ctx := context.Background()
	db := &sql.DB{}
	staticCalls := 0
	steps := make(map[string]int)
	capabilities := loginRepairCapabilities{
		dTaiwanMemberJoinInfo: true,
		memberPunishInfo:      true,
	}
	run := func(_ string, step string, _ ...interface{}) bool {
		steps[step]++
		return true
	}
	for i := 0; i < 2; i++ {
		if !cache.ensure(ctx, db, 101, func(context.Context) bool {
			staticCalls++
			return true
		}) {
			t.Fatal("static repair unexpectedly failed")
		}
		if !refreshLoginSessionWith(101, "127.0.0.1", capabilities, run) {
			t.Fatal("session refresh unexpectedly failed")
		}
	}

	if staticCalls != 1 {
		t.Fatalf("static repair calls got %d want 1", staticCalls)
	}
	if steps["clear trade punish"] != 2 {
		t.Fatalf("trade punish repair calls got %d want 2", steps["clear trade punish"])
	}
}

func TestLoginStaticRepairCacheSharesConcurrentRepair(t *testing.T) {
	var cache loginStaticRepairCache
	ctx := context.Background()
	db := &sql.DB{}
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	repair := func(context.Context) bool {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return true
	}

	const callers = 32
	results := make(chan bool, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- cache.ensure(ctx, db, 101, repair)
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	for ok := range results {
		if !ok {
			t.Fatal("shared repair unexpectedly failed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent repair calls got %d want 1", calls.Load())
	}
}

func TestLoginStaticRepairCacheWaitHonorsContext(t *testing.T) {
	var cache loginStaticRepairCache
	db := &sql.DB{}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- cache.ensure(context.Background(), db, 101, func(context.Context) bool {
			close(started)
			<-release
			return true
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cache.ensure(ctx, db, 101, func(context.Context) bool { return true }) {
		t.Fatal("cancelled waiter unexpectedly succeeded")
	}

	close(release)
	if !<-done {
		t.Fatal("shared repair unexpectedly failed")
	}
}
