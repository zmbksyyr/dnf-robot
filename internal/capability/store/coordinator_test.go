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
	data, err := json.Marshal(maps)
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
		{StoreReasonErr052, "store_err_0x52"},
		{StoreReasonErr052Zone, "store_err_0x52_zone"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("store fact constant got %q want %q", tt.got, tt.want)
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
	points := BuildGridPoints([]shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 200, YMax: 440, Use: true}})
	if len(points) == 0 {
		t.Fatalf("expected generated store points")
	}
	for _, pt := range points {
		if pt.Y < 320 {
			t.Fatalf("generated upper-half point y=%d", pt.Y)
		}
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
	c.Report(1001, first, 1, false, "test_failed")
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
	c.Report(1001, first, 1, false, "test_failed")
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

func TestStorePointCoordinatorKeepsActiveSuccessClaimed(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, 1, true, "test_success")
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
	c.regionClaims[first.Region] = claim
	c.pointMu.Unlock()
	third, ok := c.Claim(1003)
	if !ok {
		t.Fatalf("third claim failed")
	}
	if third.PointID != first.PointID || third.Source != "grid_success" {
		t.Fatalf("claim got point=%s source=%s want successful point=%s source=grid_success", third.PointID, third.Source, first.PointID)
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
	c.Report(1001, first, 1, true, "test_success")
	c.pointMu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.regionClaims[first.Region] = claim
	c.pointMu.Unlock()
	retry, ok := c.Claim(1002)
	if !ok {
		t.Fatalf("success fallback claim failed")
	}
	c.Report(1002, retry, 1, false, "transient_failed")
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
	c.Report(1001, first, 1, true, "test_success")
	c.pointMu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.regionClaims[first.Region] = claim
	c.pointMu.Unlock()
	retry, ok := c.Claim(1002)
	if !ok {
		t.Fatalf("success retry claim failed")
	}
	c.Report(1002, retry, 1, false, "store_failed")
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

func TestStorePointCoordinatorDoesNotReuseRecentStoreAckRegionAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []shared.MapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := NewPointCoordinator(configDir, nil)
	first, ok := c.Claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.Report(1001, first, 1, true, "store_ack")
	c.Flush()

	reloaded := NewPointCoordinator(configDir, nil)
	next, ok := reloaded.Claim(1002)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.Region == first.Region {
		t.Fatalf("recent store_ack region was reused: %s", first.Region)
	}
}
