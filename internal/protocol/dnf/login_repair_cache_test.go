package dnf

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginStaticRepairCacheCachesSuccessPerDatabaseAndUID(t *testing.T) {
	var cache loginStaticRepairCache
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	calls := 0
	repair := func() bool {
		calls++
		return true
	}

	if !cache.ensure(db1, 101, repair) {
		t.Fatal("initial repair unexpectedly failed")
	}
	if !cache.ensure(db1, 101, repair) {
		t.Fatal("cached repair unexpectedly failed")
	}
	if calls != 1 {
		t.Fatalf("same database and UID repair calls got %d want 1", calls)
	}
	if !cache.ensure(db1, 102, repair) || !cache.ensure(db2, 101, repair) {
		t.Fatal("independent repair unexpectedly failed")
	}
	if calls != 3 {
		t.Fatalf("independent repair calls got %d want 3", calls)
	}
}

func TestLoginStaticRepairCacheExpiresSuccess(t *testing.T) {
	now := time.Unix(1000, 0)
	cache := loginStaticRepairCache{
		ttl: time.Minute,
		now: func() time.Time { return now },
	}
	db := &sql.DB{}
	calls := 0
	repair := func() bool {
		calls++
		return true
	}

	if !cache.ensure(db, 101, repair) {
		t.Fatal("initial repair unexpectedly failed")
	}
	now = now.Add(time.Minute)
	if !cache.ensure(db, 101, repair) {
		t.Fatal("expired repair unexpectedly failed")
	}
	if calls != 2 {
		t.Fatalf("expired repair calls got %d want 2", calls)
	}
}

func TestLoginStaticRepairCacheRetriesFailure(t *testing.T) {
	var cache loginStaticRepairCache
	db := &sql.DB{}
	calls := 0
	repair := func() bool {
		calls++
		return calls > 1
	}

	if cache.ensure(db, 101, repair) {
		t.Fatal("first repair unexpectedly succeeded")
	}
	if !cache.ensure(db, 101, repair) {
		t.Fatal("successful retry failed")
	}
	if !cache.ensure(db, 101, repair) {
		t.Fatal("successful retry was not cached")
	}
	if calls != 2 {
		t.Fatalf("repair calls got %d want 2", calls)
	}
}

func TestLoginStaticCacheDoesNotSkipMutableSessionRepairs(t *testing.T) {
	var cache loginStaticRepairCache
	db := &sql.DB{}
	staticCalls := 0
	steps := make(map[string]int)
	expRepairs := 0
	capabilities := loginRepairCapabilities{
		dTaiwanMemberJoinInfo: true,
		memberPunishInfo:      true,
	}
	run := func(_ string, step string, _ ...interface{}) bool {
		steps[step]++
		return true
	}
	repairExp := func() bool {
		expRepairs++
		return true
	}

	for i := 0; i < 2; i++ {
		if !cache.ensure(db, 101, func() bool {
			staticCalls++
			return true
		}) {
			t.Fatal("static repair unexpectedly failed")
		}
		if !refreshLoginSessionWith(101, "127.0.0.1", capabilities, run, repairExp) {
			t.Fatal("session refresh unexpectedly failed")
		}
	}

	if staticCalls != 1 {
		t.Fatalf("static repair calls got %d want 1", staticCalls)
	}
	if steps["clear trade punish"] != 2 {
		t.Fatalf("trade punish repair calls got %d want 2", steps["clear trade punish"])
	}
	if expRepairs != 2 {
		t.Fatalf("exp repair calls got %d want 2", expRepairs)
	}
}

func TestLoginStaticRepairCacheSharesConcurrentRepair(t *testing.T) {
	var cache loginStaticRepairCache
	db := &sql.DB{}
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	repair := func() bool {
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
			results <- cache.ensure(db, 101, repair)
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
