package store

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"robot/internal/shared"
)

func writeStoreMapCatalog(t *testing.T, configDir string, maps []shared.MapCatalogItem) []byte {
	t.Helper()
	normalized := append([]shared.MapCatalogItem(nil), maps...)
	for i := range normalized {
		if normalized[i].XMin <= 0 {
			delta := 1 - normalized[i].XMin
			normalized[i].XMin += delta
			normalized[i].XMax += delta
		}
		if normalized[i].YMin <= 0 {
			delta := 1 - normalized[i].YMin
			normalized[i].YMin += delta
			normalized[i].YMax += delta
		}
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "pvf_map_catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStorePointFactConstants(t *testing.T) {
	tests := []struct {
		got  string
		want string
	}{
		{PointStatusUnknown, "unknown"},
		{PointStatusSuccess, "success"},
		{PointStatusFailed, "failed"},
		{PointSourceUnknown, "grid_unknown"},
		{PointSourceSuccess, "grid_success"},
		{PointSourceFailedRetry, "grid_failed_retry"},
		{StoreReasonAck, "store_ack"},
		{StoreReasonDisjointAck, "disjoint_ack"},
		{StoreReasonFailed, "store_failed"},
		{StoreReasonOnlineFailed, "store_online_failed"},
		{StoreReasonOnlineAttemptFailed, "online_failed"},
		{StoreReasonStartFailed, "store_start_failed"},
		{StoreReasonNotConfirmed, "store_not_confirmed"},
		{StoreReasonPrepareFailed, "prepare_failed"},
		{StoreReasonSetAreaFailed, "set_area_failed"},
		{StoreReasonCancelled, "cancelled"},
		{StoreReasonRuntimeStopped, "runtime_stopped"},
		{StoreReasonDisplayWaitFailed, "display_wait_failed"},
		{StoreReasonInventoryNotReady, "store_inventory_not_ready"},
		{StoreReasonErr011, "store_err_0x11"},
		{StoreReasonErr052, "store_err_0x52"},
		{StoreReasonErr052Zone, "store_err_0x52_zone"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("store fact constant got %q want %q", tt.got, tt.want)
		}
	}
}

func TestStoreErrReasonRetryClassification(t *testing.T) {
	if got := StoreErrReason(0x11); got != StoreReasonErr011 {
		t.Fatalf("StoreErrReason(0x11) got %q want %q", got, StoreReasonErr011)
	}
	tests := []struct {
		reason string
		want   bool
	}{
		{reason: "store_err_0x38", want: true},
		{reason: "store_err_0x3e", want: true},
		{reason: StoreReasonErr052, want: true},
		{reason: StoreReasonErr011, want: false},
		{reason: "store_err_0x7f", want: false},
		{reason: StoreReasonRuntimeStopped, want: false},
	}
	for _, tt := range tests {
		if got := RetryStoreReasonWithNewPoint(tt.reason); got != tt.want {
			t.Fatalf("RetryStoreReasonWithNewPoint(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

func TestStoreErr011DoesNotPollutePointExploration(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, false, StoreReasonErr011)
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID != first.PointID || next.Source != PointSourceUnknown {
		t.Fatalf("0x11 polluted point state: got point=%s source=%s want point=%s source=%s", next.PointID, next.Source, first.PointID, PointSourceUnknown)
	}
}

func TestStoreRuntimeFailureDoesNotPollutePointExploration(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, false, StoreReasonRuntimeStopped)
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID != first.PointID || next.Source != PointSourceUnknown {
		t.Fatalf("runtime failure polluted point state: got point=%s source=%s", next.PointID, next.Source)
	}
}

func TestBuildStoreGridPointsExcludesKnownBadStoreAreas(t *testing.T) {
	points := BuildGridPoints([]shared.MapCatalogItem{
		{Village: 2, Area: 3, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 9, Area: 3, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 11, Area: 0, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 11, Area: 1, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 14, Area: 0, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 14, Area: 1, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 3, Area: 0, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
	})
	if len(points) != 1 || points[0].Village != 3 || points[0].Area != 0 {
		t.Fatalf("bad store areas were not filtered: %+v", points)
	}
}

func TestBuildStoreGridPointsExcludesZeroCoordinates(t *testing.T) {
	points := BuildGridPoints([]shared.MapCatalogItem{{
		Village: 3, Area: 0, XMin: 0, XMax: PointXStep * 2,
		YMin: 200, YMax: 200, Use: true,
	}})
	if len(points) != 2 {
		t.Fatalf("points got %d want 2: %+v", len(points), points)
	}
	for _, point := range points {
		if point.X <= 0 || point.Y <= 0 {
			t.Fatalf("generated invalid coordinate: %+v", point)
		}
	}
}

func TestStorePointCoordinatorCachesSourceMD5(t *testing.T) {
	configDir := t.TempDir()
	data := writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 120, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	if len(c.points) == 0 {
		t.Fatalf("expected generated store points")
	}
	cacheData, err := os.ReadFile(filepath.Join(configDir, PointCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	var cache PointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	if cache.SourceMD5 != hex.EncodeToString(sum[:]) {
		t.Fatalf("cache md5 got %q want source md5", cache.SourceMD5)
	}
}

func TestBuildStoreGridPointsUsesLowerHalf(t *testing.T) {
	points := BuildGridPoints([]shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 1, XMax: 1, YMin: 200, YMax: 440, Use: true}})
	if len(points) == 0 {
		t.Fatalf("expected generated store points")
	}
	for _, pt := range points {
		if pt.Y < 320 {
			t.Fatalf("generated upper-half point y=%d", pt.Y)
		}
	}
}

func TestBuildStoreGridPointsExcludesUnsafeVillageArea(t *testing.T) {
	points := BuildGridPoints([]shared.MapCatalogItem{
		{Village: 3, Area: 1, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
		{Village: 3, Area: 0, XMin: 1, XMax: 1, YMin: 1, YMax: 1, Use: true},
	})
	if len(points) != 1 {
		t.Fatalf("points got %d want 1", len(points))
	}
	if points[0].Village != 3 || points[0].Area != 0 {
		t.Fatalf("unsafe area was not filtered: %+v", points[0])
	}
}

func TestStorePointCoordinatorFiltersUnsafeCachedPoint(t *testing.T) {
	configDir := t.TempDir()
	data := writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	sum := md5.Sum(data)
	cache := PointCache{
		Version: PointCacheVer, SourceFile: "pvf_map_catalog.json", SourceMD5: hex.EncodeToString(sum[:]),
		XStep: PointXStep, YStep: PointYStep,
		Points: []GridPoint{
			{ID: "3-1-0-0", Village: 3, Area: 1, X: 0, Y: 0},
			{ID: "3-0-0-0", Village: 3, Area: 0, X: 0, Y: 0},
		},
	}
	cacheData, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, PointCacheFile), cacheData, 0644); err != nil {
		t.Fatal(err)
	}
	c := NewPointCoordinator(configDir, nil)
	pos, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("claim failed")
	}
	if pos.Village != 3 || pos.Area != 0 {
		t.Fatalf("claimed unsafe cached point: %+v", pos)
	}
}

func TestStorePointCoordinatorDoesNotReuseFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, false, "store_err_0x38")
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("failed point was reused after restart: %s", next.PointID)
	}
}

func TestStorePointCoordinatorRetriesOldFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, false, "store_err_0x38")
	c.Flush()

	cachePath := filepath.Join(configDir, PointCacheFile)
	cacheData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache PointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	cache.Points[0].LastResultAt = time.Now().Add(-PointFailRetry - time.Minute).Format(time.RFC3339)
	cacheData, err = json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, cacheData, 0644); err != nil {
		t.Fatal(err)
	}

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("old failed point was not retried")
	}
	if next.PointID != first.PointID || next.Source != "grid_failed_retry" {
		t.Fatalf("claim got point=%s source=%s want point=%s source=grid_failed_retry", next.PointID, next.Source, first.PointID)
	}
}

func TestStorePointCoordinatorRetriesCollisionAfterActiveLease(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	lease := StorePointLeaseDuration(210)
	first, ok := c.ClaimWithLease(1001, lease)
	if !ok {
		t.Fatal("first claim failed")
	}
	c.Report(1001, first, false, "store_err_0x38")
	c.pointMu.Lock()
	c.points[c.byID[first.PointID]].LastResultAt = time.Now().Add(-lease - time.Second).Format(time.RFC3339)
	c.pointMu.Unlock()

	next, ok := c.ClaimWithLease(1002, lease)
	if !ok || next.PointID != first.PointID || next.Source != PointSourceFailedRetry {
		t.Fatalf("collision retry got point=%+v ok=%v", next, ok)
	}
}

func TestStorePointCoordinatorPermanentlySkipsRestrictivePoint(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatal("first claim failed")
	}
	c.Report(1001, first, false, StoreReasonErr052)
	c.pointMu.Lock()
	c.points[c.byID[first.PointID]].LastResultAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	c.pointMu.Unlock()

	if next, ok := c.Claim(1002); ok {
		t.Fatalf("restrictive point was retried: %+v", next)
	}
}

func TestStorePointCoordinatorPrefersKnownSuccessOverPackedUnknown(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 400, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	offGrid := Position{Village: 3, Area: 0, X: 1, Y: 281, PointID: "3-0-1-281"}
	c.Report(1001, offGrid, true, StoreReasonAck)
	c.ReleaseUID(1001)

	next, ok := c.Claim(1002)
	if !ok {
		t.Fatal("packed point claim failed")
	}
	if next.PointID != offGrid.PointID || next.Source != PointSourceSuccess {
		t.Fatalf("claim got point=%s source=%s, want known success point", next.PointID, next.Source)
	}
}

func TestBuildPackedPointSetShiftsAroundPermanentRestriction(t *testing.T) {
	points := []GridPoint{
		{ID: "0", Village: 3, Area: 0, X: 0, Y: 0, LastReason: StoreReasonErr052},
		{ID: "120", Village: 3, Area: 0, X: 120, Y: 0},
		{ID: "240", Village: 3, Area: 0, X: 240, Y: 0},
		{ID: "360", Village: 3, Area: 0, X: 360, Y: 0},
	}
	packed := buildPackedPointSet(points)
	if packed["0"] || !packed["120"] || !packed["240"] || !packed["360"] {
		t.Fatalf("packed set = %+v, want 120, 240, and 360", packed)
	}
}

func TestStorePointCoordinatorKeepsActiveSuccessClaimed(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, "test_success")
	second, ok := c.Claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if second.PointID == first.PointID {
		t.Fatalf("active success point was immediately reused: %s", first.PointID)
	}
	c.pointMu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.pointMu.Unlock()
	third, ok := c.Claim(1003)
	if !ok {
		t.Fatalf("third claim failed")
	}
	if third.PointID != first.PointID || third.Source != "grid_success" {
		t.Fatalf("claim got point=%s source=%s want successful point=%s source=grid_success", third.PointID, third.Source, first.PointID)
	}
}

func TestStorePointCoordinatorBlocksVerticallyNearbyPoint(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 240, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatal("first claim failed")
	}
	if first.Y != 121 {
		t.Fatalf("first point y got %d want 121", first.Y)
	}
	if next, ok := c.Claim(1002); ok {
		t.Fatalf("vertically nearby point was claimed: first=%+v next=%+v", first, next)
	}
}

func TestStorePointCoordinatorAllowsEveryOtherGeneratedRow(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 480, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatal("first claim failed")
	}
	second, ok := c.Claim(1002)
	if !ok || second.Y-first.Y != PointYStep*2 {
		t.Fatalf("every-other generated row was unavailable: first=%+v second=%+v ok=%v", first, second, ok)
	}
}

func TestStorePointCoordinatorAllowsAdjacentGeneratedColumn(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: PointXStep, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatal("first claim failed")
	}
	second, ok := c.Claim(1002)
	if !ok || second.X-first.X != PointXStep {
		t.Fatalf("adjacent generated column was unavailable: first=%+v second=%+v ok=%v", first, second, ok)
	}
}

func TestStorePointCoordinatorKeepsHistoricallySuccessfulPointReusable(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, "test_success")
	c.pointMu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.pointMu.Unlock()
	retry, ok := c.Claim(1002)
	if !ok {
		t.Fatalf("success fallback claim failed")
	}
	c.Report(1002, retry, false, "transient_failed")
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1003)
	if !ok {
		t.Fatalf("historically successful point was not reusable after restart")
	}
	if next.PointID != first.PointID {
		t.Fatalf("claim got %s want historical success point %s", next.PointID, first.PointID)
	}
}

func TestStorePointCoordinatorCoolsDownRecentlyFailedSuccessPoint(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, "test_success")
	c.pointMu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.pointMu.Unlock()
	retry, ok := c.Claim(1002)
	if !ok {
		t.Fatalf("success retry claim failed")
	}
	c.Report(1002, retry, false, "store_err_0x38")
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1003)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("recently failed success point was reused: %s", first.PointID)
	}
}

func TestStorePointCoordinatorSuccessClearsFailedIndex(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, false, "store_err_0x38")
	c.pointMu.Lock()
	c.points[c.byID[first.PointID]].LastResultAt = time.Now().Add(-PointFailRetry - time.Minute).Format(time.RFC3339)
	c.pointMu.Unlock()

	retry, ok := c.Claim(1002)
	if !ok || retry.Source != PointSourceFailedRetry {
		t.Fatalf("failed retry got point=%+v ok=%v", retry, ok)
	}
	c.Report(1002, retry, true, StoreReasonAck)
	c.ReleaseUID(1002)

	next, ok := c.Claim(1003)
	if !ok {
		t.Fatalf("success claim failed")
	}
	if next.PointID != first.PointID || next.Source != PointSourceSuccess {
		t.Fatalf("successful retry remained failed: got point=%s source=%s", next.PointID, next.Source)
	}
}

func TestStorePointCoordinatorReusesPersistedStoreAckAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, "store_ack")
	c.ReleaseUID(1001)
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.PointID != first.PointID || next.Source != PointSourceSuccess {
		t.Fatalf("persisted store point got point=%s source=%s, want %s", next.PointID, next.Source, first.PointID)
	}
}

func TestStorePointCoordinatorReusesPersistedDisjointAckAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, StoreReasonDisjointAck)
	c.ReleaseUID(1001)
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.PointID != first.PointID || next.Source != PointSourceSuccess {
		t.Fatalf("persisted disjoint point got point=%s source=%s, want %s", next.PointID, next.Source, first.PointID)
	}
}

func TestStorePointLeaseCoversStoreDurationAndCleanup(t *testing.T) {
	if got, want := StorePointLeaseDuration(210), 4*time.Minute+35*time.Second; got != want {
		t.Fatalf("StorePointLeaseDuration(210) = %s, want %s", got, want)
	}
	if got := StorePointLeaseDuration(10); got != pointClaimTTL {
		t.Fatalf("short StorePointLeaseDuration = %s, want minimum %s", got, pointClaimTTL)
	}
}

func TestStorePointCoordinatorReleasesSuccessfulLeaseByUID(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.ClaimWithLease(1001, 10*time.Minute)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, true, StoreReasonAck)
	if _, ok := c.ClaimWithLease(1002, 10*time.Minute); ok {
		t.Fatalf("active successful lease was reused")
	}
	c.ReleaseUID(1001)
	next, ok := c.ClaimWithLease(1002, 10*time.Minute)
	if !ok {
		t.Fatalf("released successful lease was not reusable")
	}
	if next.PointID != first.PointID {
		t.Fatalf("claim got %s, want released point %s", next.PointID, first.PointID)
	}
}

func TestStorePointCoordinatorFlushAtomicallyReplacesCache(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	pos, ok := c.Claim(1001)
	if !ok {
		t.Fatal("claim failed")
	}
	c.Report(1001, pos, true, StoreReasonAck)
	c.Flush()
	c.Report(1001, pos, true, StoreReasonAck)
	c.Flush()

	cachePath := filepath.Join(configDir, PointCacheFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache PointCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("cache is invalid JSON: %v", err)
	}
	if _, err := os.Stat(cachePath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary cache file remains: %v", err)
	}
}

func TestStorePointCoordinatorTreatsRepeatedIndependentFailureAsSessionScoped(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	var failures AttemptFailureState
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatal("first claim failed")
	}
	if sessionFailure := c.ReportAttemptFailure(1001, &failures, first, "store_err_0x38"); sessionFailure {
		t.Fatal("first observation was classified as a session failure")
	}
	second, ok := c.Claim(1001)
	if !ok {
		t.Fatal("second independent claim failed")
	}
	if positionsConflictPosition(first, second) {
		t.Fatalf("second point conflicts with the pending probe: first=%+v second=%+v", first, second)
	}
	if sessionFailure := c.ReportAttemptFailure(1001, &failures, second, "store_err_0x38"); !sessionFailure {
		t.Fatal("repeated independent failure was not classified as session-scoped")
	}
	if len(c.pointClaims) != 0 {
		t.Fatalf("session failure left %d point claims", len(c.pointClaims))
	}
	if got := c.points[c.byID[first.PointID]].LastReason; got != "" {
		t.Fatalf("first probe polluted point history with %q", got)
	}
	if got := c.points[c.byID[second.PointID]].LastReason; got != "" {
		t.Fatalf("second probe polluted point history with %q", got)
	}
	if len(c.pointEvidence) != 0 || len(c.pointCooldown) != 0 {
		t.Fatalf("session failure left evidence=%d cooldown=%d", len(c.pointEvidence), len(c.pointCooldown))
	}
}

func TestStorePointCoordinatorKeepsDeferredFailureOutOfCache(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	var failures AttemptFailureState
	first, _ := c.Claim(1001)
	c.ReportAttemptFailure(1001, &failures, first, "store_err_0x38")
	second, ok := c.Claim(1001)
	if !ok {
		t.Fatal("success point claim failed")
	}
	c.CommitAttemptFailure(1001, &failures)
	c.Report(1001, second, true, StoreReasonAck)
	if got := c.points[c.byID[first.PointID]].LastReason; got != "" {
		t.Fatalf("deferred failure polluted point history with %q", got)
	}
	if got := c.points[c.byID[second.PointID]].LastReason; got != StoreReasonAck {
		t.Fatalf("success reason got %q", got)
	}
}

func TestStorePointCoordinatorCoolsAmbiguousPointAcrossSessions(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	lease := 5 * time.Minute
	firstPointID := ""
	for uid := 1001; uid <= 1003; uid++ {
		var failures AttemptFailureState
		pos, ok := c.ClaimWithLease(uid, lease)
		if !ok {
			t.Fatalf("uid %d claim failed", uid)
		}
		if firstPointID == "" {
			firstPointID = pos.PointID
		} else if pos.PointID != firstPointID {
			t.Fatalf("uid %d got point %s, want repeated point %s", uid, pos.PointID, firstPointID)
		}
		c.ReportAttemptFailure(uid, &failures, pos, "disjoint_err_0xbe")
		c.CommitAttemptFailure(uid, &failures)
	}

	next, ok := c.ClaimWithLease(1004, lease)
	if !ok {
		t.Fatal("fourth session claim failed")
	}
	if next.PointID == firstPointID {
		t.Fatalf("fourth session reused cooled point %s", firstPointID)
	}
	if _, ok := c.pointCooldown[firstPointID]; !ok {
		t.Fatalf("point %s was not cooled after three distinct sessions", firstPointID)
	}
	if got := c.points[c.byID[firstPointID]].LastReason; got != "" {
		t.Fatalf("evidence cooldown polluted point history with %q", got)
	}
	c.Discard(1004, next)
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	if got := reloaded.points[reloaded.byID[firstPointID]].LastReason; got != "" {
		t.Fatalf("evidence cooldown persisted reason %q", got)
	}
}

func TestNormalizeAmbiguousFailureBursts(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	points := []GridPoint{
		{ID: "a", Village: 3, Area: 0, X: 0, Y: 0, Status: PointStatusFailed, Failed: 1, LastUID: 1001, LastReason: "store_err_0x3e", LastResultAt: now.Format(time.RFC3339)},
		{ID: "b", Village: 4, Area: 0, X: 0, Y: 0, Status: PointStatusFailed, Failed: 1, LastUID: 1001, LastReason: "store_err_0x3e", LastResultAt: now.Add(10 * time.Second).Format(time.RFC3339)},
		{ID: "zone", Village: 5, Area: 0, X: 0, Y: 0, Status: PointStatusFailed, Failed: 1, LastUID: 1001, LastReason: StoreReasonErr052Zone, LastResultAt: now.Format(time.RFC3339)},
	}
	if got := normalizeAmbiguousFailureBursts(points); got != 2 {
		t.Fatalf("pruned points got %d want 2", got)
	}
	for _, index := range []int{0, 1} {
		if points[index].Status != PointStatusUnknown || points[index].LastReason != "" || points[index].Failed != 0 {
			t.Fatalf("point %d was not restored: %+v", index, points[index])
		}
	}
	if points[2].LastReason != StoreReasonErr052Zone || points[2].Status != PointStatusFailed {
		t.Fatalf("restrictive zone was pruned: %+v", points[2])
	}
}

func TestNormalizeAmbiguousFailureBurstKeepsOneConflictZone(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	points := []GridPoint{
		{ID: "a", Village: 3, Area: 0, X: 0, Y: 0, Status: PointStatusFailed, Failed: 1, LastUID: 1001, LastReason: "disjoint_err_0xbe", LastResultAt: now.Format(time.RFC3339)},
		{ID: "b", Village: 3, Area: 0, X: pointOccupancyConflictX, Y: 80, Status: PointStatusFailed, Failed: 1, LastUID: 1001, LastReason: "disjoint_err_0xbe", LastResultAt: now.Add(5 * time.Second).Format(time.RFC3339)},
	}
	if got := normalizeAmbiguousFailureBursts(points); got != 1 {
		t.Fatalf("pruned points got %d want 1", got)
	}
	if points[0].LastReason != "disjoint_err_0xbe" || points[1].LastReason != "" {
		t.Fatalf("conflict-zone normalization got first=%+v second=%+v", points[0], points[1])
	}
}

func TestStorePointCoordinatorKeepsRequestedLeaseAfterSuccess(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	lease := 10 * time.Minute
	pos, ok := c.ClaimWithLease(1001, lease)
	if !ok {
		t.Fatalf("claim failed")
	}
	c.Report(1001, pos, true, StoreReasonAck)
	c.pointMu.Lock()
	claim := c.pointClaims[pos.PointID]
	c.pointMu.Unlock()
	remaining := time.Until(claim.ExpiresAt)
	if claim.Lease != lease || remaining < lease-time.Second {
		t.Fatalf("success claim lease=%s remaining=%s, want about %s", claim.Lease, remaining, lease)
	}
}
