package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectStoreItemsUsesAllowDenyAndMaterialRules(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 3037, Level: 1, Slot: "material", Trade: true, BasicMaterial: true, Icon: "stackable/material.img", FieldImage: "material/ore", StackLimit: 1000},
		{ID: 3031, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/cloth", StackLimit: 1000},
		{ID: 3032, Level: 99, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/high", StackLimit: 1000},
		{ID: 7312, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/deny", StackLimit: 1000},
		{ID: 3034, Level: 1, Slot: "material", Trade: true, Icon: "stackable/etc.img", FieldImage: "material/bad_icon", StackLimit: 1000},
		{ID: 3035, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 7,
		StoreItemAllowIDs:      []int{3037, 3031, 3032, 3034, 3035, 7312},
		StoreItemDenyIDs:       []int{7312},
	})

	got := storeItemIDSet(items)
	if len(got) != 1 || !got[3037] {
		t.Fatalf("selected IDs got %v want only basic allowed material 3037", got)
	}
}

func TestSelectStoreItemsFallbacksToAllowIDs(t *testing.T) {
	m := testRobotManagerWithStackableCatalog(t, []equipmentCatalogItem{
		{ID: 9001, Level: 1, Slot: "material", Trade: true, Icon: "stackable/material.img", FieldImage: "material/not_allowed", StackLimit: 1000},
	})

	items := m.selectStoreItems(RobotInfo{Level: 10}, robotRuntimeConfig{
		StoreItemSlots:         4,
		StoreInventoryStartBox: 7,
		StoreItemAllowIDs:      []int{3037, 3031},
		StoreItemDenyIDs:       []int{3031},
	})

	if len(items) != 1 || items[0].ID != 3037 || items[0].Slot != "material" {
		t.Fatalf("fallback items got %+v want synthetic material 3037", items)
	}
}

func TestStorePointCoordinatorCachesSourceMD5(t *testing.T) {
	configDir := t.TempDir()
	data := writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 120, Use: true}})
	c := newStorePointCoordinator(configDir)
	if len(c.points) == 0 {
		t.Fatalf("expected generated store points")
	}
	cacheData, err := os.ReadFile(filepath.Join(configDir, storePointCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	var cache storePointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	if cache.SourceMD5 != hex.EncodeToString(sum[:]) {
		t.Fatalf("cache md5 got %q want source md5", cache.SourceMD5)
	}
}

func TestStorePointCoordinatorDoesNotReuseFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, false, "test_failed")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("failed point was reused after restart: %s", next.PointID)
	}
}

func TestStorePointCoordinatorRetriesOldFailedPointAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, false, "test_failed")
	c.flush()

	cachePath := filepath.Join(configDir, storePointCacheFile)
	cacheData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache storePointCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatal(err)
	}
	cache.Points[0].LastResultAt = time.Now().Add(-storePointFailRetry - time.Minute).Format(time.RFC3339)
	cacheData, err = json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, cacheData, 0644); err != nil {
		t.Fatal(err)
	}

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1002)
	if !ok {
		t.Fatalf("old failed point was not retried")
	}
	if next.PointID != first.PointID || next.Source != "grid_failed_retry" {
		t.Fatalf("claim got point=%s source=%s want point=%s source=grid_failed_retry", next.PointID, next.Source, first.PointID)
	}
}

func TestStorePointCoordinatorKeepsActiveSuccessClaimed(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "test_success")
	second, ok := c.claim(1002)
	if !ok {
		t.Fatalf("second claim failed")
	}
	if second.PointID == first.PointID {
		t.Fatalf("active success point was immediately reused: %s", first.PointID)
	}
	c.mu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.regionClaims[first.Region] = claim
	c.mu.Unlock()
	third, ok := c.claim(1003)
	if !ok {
		t.Fatalf("third claim failed")
	}
	if third.PointID != first.PointID || third.Source != "grid_success" {
		t.Fatalf("claim got point=%s source=%s want successful point=%s source=grid_success", third.PointID, third.Source, first.PointID)
	}
}

func TestStorePointCoordinatorKeepsHistoricallySuccessfulPointReusable(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 0, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "test_success")
	c.mu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.regionClaims[first.Region] = claim
	c.mu.Unlock()
	retry, ok := c.claim(1002)
	if !ok {
		t.Fatalf("success fallback claim failed")
	}
	c.report(1002, retry, 1, false, "transient_failed")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1003)
	if !ok {
		t.Fatalf("historically successful point was not reusable after restart")
	}
	if next.PointID != first.PointID {
		t.Fatalf("claim got %s want historical success point %s", next.PointID, first.PointID)
	}
}

func TestStorePointCoordinatorCoolsDownRecentlyFailedSuccessPoint(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "test_success")
	c.mu.Lock()
	claim := c.pointClaims[first.PointID]
	claim.ExpiresAt = time.Now().Add(-time.Second)
	c.pointClaims[first.PointID] = claim
	c.regionClaims[first.Region] = claim
	c.mu.Unlock()
	retry, ok := c.claim(1002)
	if !ok {
		t.Fatalf("success retry claim failed")
	}
	c.report(1002, retry, 1, false, "store_failed")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1003)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.PointID == first.PointID {
		t.Fatalf("recently failed success point was reused: %s", first.PointID)
	}
}

func TestStorePointCoordinatorDoesNotReuseRecentStoreAckRegionAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	writeStoreMapCatalog(t, configDir, []mapCatalogItem{{Village: 3, Area: 0, XMin: 0, XMax: 360, YMin: 0, YMax: 0, Use: true}})
	c := newStorePointCoordinator(configDir)
	first, ok := c.claim(1001)
	if !ok {
		t.Fatalf("first claim failed")
	}
	c.report(1001, first, 1, true, "store_ack")
	c.flush()

	reloaded := newStorePointCoordinator(configDir)
	next, ok := reloaded.claim(1002)
	if !ok {
		t.Fatalf("next claim failed")
	}
	if next.Region == first.Region {
		t.Fatalf("recent store_ack region was reused: %s", first.Region)
	}
}
