package store

import (
	"sync"
	"testing"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

func TestStorePointCoordinatorKeepsSuccessfulOccupancyAfterClaimExpires(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	lease := 10 * time.Minute
	position, ok := coordinator.ClaimWithLease(1001, lease)
	if !ok {
		t.Fatal("initial claim failed")
	}
	coordinator.Report(1001, position, true, StoreReasonAck)

	coordinator.pointMu.Lock()
	claim := coordinator.pointClaims[position.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	coordinator.pointClaims[position.PointID] = claim
	coordinator.pointMu.Unlock()

	if next, ok := coordinator.ClaimWithLease(1002, lease); ok {
		t.Fatalf("recent successful occupancy was reused: %+v", next)
	}
}

func TestStorePointCoordinatorRestoresActiveOccupancyAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	position, ok := coordinator.ClaimForStore(1001, 210)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(1001, position, true, StoreReasonAck)

	reloaded := NewPointCoordinator(configDir, nil)
	if len(reloaded.pointClaims) != 0 {
		t.Fatalf("restart restored stale claim ownership: %+v", reloaded.pointClaims)
	}
	if next, ok := reloaded.ClaimForStore(1002, 210); ok {
		t.Fatalf("active store point was reused after restart: %+v", next)
	}
}

func TestStorePointCoordinatorRestoredOccupancyBlocksNearbyPoint(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{
		Village: 3, Area: 0, XMin: 1, XMax: 1, YMin: 1, YMax: 161, Use: true,
	}})
	coordinator := NewPointCoordinator(configDir, nil)
	position, ok := coordinator.ClaimForStore(1001, 210)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(1001, position, true, StoreReasonAck)

	reloaded := NewPointCoordinator(configDir, nil)
	if next, ok := reloaded.ClaimForStore(1002, 210); ok {
		t.Fatalf("nearby point ignored restored occupancy: active=%+v next=%+v", position, next)
	}
}

func TestStorePointCoordinatorPersistsActiveOccupancyRelease(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	position, ok := coordinator.ClaimForStore(1001, 210)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(1001, position, true, StoreReasonAck)
	coordinator.ReleaseUID(1001)

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.ClaimForStore(1002, 210)
	if !ok {
		t.Fatal("released store point remained occupied after restart")
	}
	if next.PointID != position.PointID {
		t.Fatalf("released point = %+v, want %s", next, position.PointID)
	}
}

func TestStorePointCoordinatorReusesStorePointAtExactUIDExpiry(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	const (
		uid         = 1001
		durationSec = 210
	)
	position, ok := coordinator.ClaimForStore(uid, durationSec)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(uid, position, true, StoreReasonAck)

	coordinator.pointMu.Lock()
	claim := coordinator.pointClaims[position.PointID]
	point := coordinator.points[coordinator.byID[position.PointID]]
	occupied := coordinator.pointOccupancy[areaKey{point.Village, point.Area}][occupancyCellFor(point.X, point.Y)][position.PointID]
	coordinator.pointMu.Unlock()

	wantReuse := robotconfig.StoreDurationForUID(durationSec, uid)
	if claim.ReuseAfter != wantReuse {
		t.Fatalf("reuse duration = %s, want %s", claim.ReuseAfter, wantReuse)
	}
	if !claim.ClaimUntil.IsZero() {
		t.Fatalf("successful claim still blocks through claim deadline %s", claim.ClaimUntil)
	}
	if !claim.ExpiresAt.After(occupied.SuccessUntil) {
		t.Fatalf("cleanup expiry %s must outlive reusable time %s", claim.ExpiresAt, occupied.SuccessUntil)
	}
	remaining := time.Until(occupied.SuccessUntil)
	if remaining < wantReuse-time.Second || remaining > wantReuse+time.Second {
		t.Fatalf("successful occupancy remaining = %s, want about %s", remaining, wantReuse)
	}
}

func TestStorePointCoordinatorOldUIDCannotReleaseReplacementClaim(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	first, ok := coordinator.ClaimForStore(1001, 210)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(1001, first, true, StoreReasonAck)

	coordinator.pointMu.Lock()
	coordinator.setPointSuccessOccupancyLocked(first.PointID, time.Now().Add(-time.Second))
	coordinator.pointMu.Unlock()
	second, ok := coordinator.ClaimForStore(1002, 210)
	if !ok || second.PointID != first.PointID {
		t.Fatalf("expired point replacement = %+v ok=%v", second, ok)
	}

	coordinator.ReleaseUID(1001)
	coordinator.Report(1001, first, false, StoreReasonErr052)
	coordinator.pointMu.Lock()
	claim := coordinator.pointClaims[first.PointID]
	failed := coordinator.failedPoints[first.PointID]
	coordinator.pointMu.Unlock()
	if claim.UID != 1002 {
		t.Fatalf("old UID removed replacement claim: %+v", claim)
	}
	if failed {
		t.Fatal("stale old-UID report polluted replacement point history")
	}
	if third, ok := coordinator.ClaimForStore(1003, 210); ok {
		t.Fatalf("replacement claim was no longer occupied: %+v", third)
	}
}

func TestStorePointCoordinatorExpiredPointHasSingleConcurrentWinner(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	coordinator := NewPointCoordinator(configDir, nil)
	first, ok := coordinator.ClaimForStore(1001, 210)
	if !ok {
		t.Fatal("initial store claim failed")
	}
	coordinator.Report(1001, first, true, StoreReasonAck)
	coordinator.pointMu.Lock()
	coordinator.setPointSuccessOccupancyLocked(first.PointID, time.Now().Add(-time.Second))
	coordinator.pointMu.Unlock()

	const contenders = 32
	start := make(chan struct{})
	results := make(chan bool, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			<-start
			_, claimed := coordinator.ClaimForStore(uid, 210)
			results <- claimed
		}(2000 + i)
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent winners = %d, want 1", winners)
	}
}

func BenchmarkPointCoordinatorClaimAt600Occupancies(b *testing.B) {
	points := BuildGridPoints([]shared.MapCatalogItem{{
		Village: 3,
		Area:    0,
		XMin:    0,
		XMax:    120 * 120,
		YMin:    0,
		YMax:    80 * 80,
		Use:     true,
	}})
	coordinator := NewPointCoordinator("", nil)
	coordinator.points = points
	coordinator.rebuildIndexes()
	for uid := 1; uid <= 600; uid++ {
		if _, ok := coordinator.ClaimWithLease(uid, 10*time.Minute); !ok {
			b.Fatalf("setup claim %d failed", uid)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uid := 10000 + i
		position, ok := coordinator.ClaimWithLease(uid, 10*time.Minute)
		if !ok {
			b.Fatal("benchmark claim failed")
		}
		coordinator.Discard(uid, position)
	}
}

func BenchmarkBuildPackedPointSet(b *testing.B) {
	points := BuildGridPoints([]shared.MapCatalogItem{{
		Village: 3,
		Area:    0,
		XMin:    0,
		XMax:    120 * 120,
		YMin:    0,
		YMax:    80 * 80,
		Use:     true,
	}})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if packed := buildPackedPointSet(points); len(packed) == 0 {
			b.Fatal("packed point set is empty")
		}
	}
}
