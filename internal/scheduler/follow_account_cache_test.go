package scheduler

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"robot/internal/foundation/config"
)

type followAccountTestRepository struct {
	missingSchemaRepository
	uids         []int
	village      int
	uidsCalls    atomic.Int32
	villageCalls atomic.Int32
	started      chan struct{}
	release      chan struct{}
}

func (*followAccountTestRepository) Stats() sql.DBStats                { return sql.DBStats{} }
func (*followAccountTestRepository) PingContext(context.Context) error { return nil }
func (*followAccountTestRepository) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

func (r *followAccountTestRepository) FollowAccountUIDs(string) ([]int, error) {
	if r.uidsCalls.Add(1) == 1 && r.started != nil {
		close(r.started)
	}
	if r.release != nil {
		<-r.release
	}
	return append([]int(nil), r.uids...), nil
}

func (r *followAccountTestRepository) FollowAccountVillageLastPlayed(string) (int, bool, error) {
	r.villageCalls.Add(1)
	return r.village, r.village > 0, nil
}

func TestFollowAccountLookupCoalescesConcurrentMoves(t *testing.T) {
	repo := &followAccountTestRepository{
		uids: []int{17000001}, village: 3,
		started: make(chan struct{}), release: make(chan struct{}),
	}
	manager := NewRobotManager(repo, &config.SysConfig{ConfigDir: t.TempDir()}, nil)
	t.Cleanup(func() { _ = manager.Shutdown() })

	first := make(chan followAccountLookup, 1)
	go func() {
		lookup, _ := manager.loadFollowAccount("leader")
		first <- lookup
	}()
	<-repo.started

	started := time.Now()
	if _, ok := manager.loadFollowAccount("leader"); ok {
		t.Fatal("in-flight first lookup unexpectedly returned cached data")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("concurrent lookup blocked for %s", elapsed)
	}
	if repo.uidsCalls.Load() != 1 {
		t.Fatalf("UID lookup calls during refresh = %d, want 1", repo.uidsCalls.Load())
	}

	close(repo.release)
	lookup := <-first
	if len(lookup.uids) != 1 || lookup.uids[0] != 17000001 || !lookup.villageOK || lookup.village != 3 {
		t.Fatalf("refreshed lookup = %+v", lookup)
	}
	for range 32 {
		if _, ok := manager.loadFollowAccount("leader"); !ok {
			t.Fatal("fresh lookup was not cached")
		}
	}
	if repo.uidsCalls.Load() != 1 || repo.villageCalls.Load() != 1 {
		t.Fatalf("cached lookup queries UID=%d village=%d, want 1 each", repo.uidsCalls.Load(), repo.villageCalls.Load())
	}
}
