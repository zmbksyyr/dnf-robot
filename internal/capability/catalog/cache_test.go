package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/shared"
)

func TestMapsCacheRefreshesAndReturnsCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	writeCatalogJSON(t, path, []shared.MapCatalogItem{{Village: 3, Area: 1, XMin: 10, XMax: 20}})

	first := Maps(dir)
	if len(first) != 1 || first[0].Area != 1 {
		t.Fatalf("first maps = %+v", first)
	}
	first[0].Area = 99
	if again := Maps(dir); len(again) != 1 || again[0].Area != 1 {
		t.Fatalf("cached map was mutated by caller: %+v", again)
	}

	writeCatalogJSON(t, path, []shared.MapCatalogItem{{Village: 3, Area: 2, XMin: 10, XMax: 20}})
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	refreshed := Maps(dir)
	if len(refreshed) != 1 || refreshed[0].Area != 2 {
		t.Fatalf("refreshed maps = %+v", refreshed)
	}
}

func TestMapViewReusesUnchangedCatalogAndRefreshes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	writeCatalogJSON(t, path, []shared.MapCatalogItem{{Village: 3, Area: 1}})

	first := ViewMaps(dir)
	second := ViewMaps(dir)
	if len(first) != 1 || len(second) != 1 || &first[0] != &second[0] {
		t.Fatalf("unchanged map view was not reused: first=%+v second=%+v", first, second)
	}

	writeCatalogJSON(t, path, []shared.MapCatalogItem{{Village: 3, Area: 2}, {Village: 3, Area: 3}})
	third := ViewMaps(dir)
	if len(third) != 2 || third[0].Area != 2 || third[1].Area != 3 {
		t.Fatalf("map view did not refresh: %+v", third)
	}
	if len(first) != 1 || first[0].Area != 1 {
		t.Fatalf("previous map view was mutated: %+v", first)
	}
}

func TestShoutTemplatesCacheRefreshesMissingFileAndReturnsCopies(t *testing.T) {
	dir := t.TempDir()
	missing := ShoutTemplates(dir)
	if len(missing.Messages) != 1 || missing.Messages[0] != "hello" {
		t.Fatalf("missing fallback = %+v", missing)
	}

	path := filepath.Join(dir, "robot_shout_templates.json")
	writeCatalogJSON(t, path, []string{"first", "second"})
	loaded := ShoutTemplates(dir)
	if len(loaded.Messages) != 2 || loaded.Messages[0] != "first" {
		t.Fatalf("loaded templates = %+v", loaded)
	}
	loaded.Messages[0] = "changed"
	if again := ShoutTemplates(dir); again.Messages[0] != "first" {
		t.Fatalf("cached templates were mutated by caller: %+v", again)
	}

	writeCatalogJSON(t, path, map[string]interface{}{
		"channel":  "local",
		"type":     3,
		"messages": []string{"replacement-message"},
	})
	refreshed := ShoutTemplates(dir)
	if refreshed.Channel != "local" || len(refreshed.Messages) != 1 || refreshed.Messages[0] != "replacement-message" {
		t.Fatalf("refreshed templates = %+v", refreshed)
	}
}

func TestMapsCacheConcurrentReadersReceiveIndependentSlices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	writeCatalogJSON(t, path, []shared.MapCatalogItem{{Village: 3, Area: 4}})

	const readers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(area int) {
			defer wg.Done()
			<-start
			maps := Maps(dir)
			if len(maps) != 1 || maps[0].Area != 4 {
				t.Errorf("maps = %+v", maps)
				return
			}
			maps[0].Area = area
		}(i + 10)
	}
	close(start)
	wg.Wait()
	if maps := Maps(dir); len(maps) != 1 || maps[0].Area != 4 {
		t.Fatalf("concurrent readers mutated cache: %+v", maps)
	}
}

func TestItemCatalogCacheHitDecodesOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 1001}})

	var cache jsonFileCache[[]shared.EquipmentCatalogItem]
	decodeCount := 0
	decode := func(data []byte, fallback []shared.EquipmentCatalogItem) []shared.EquipmentCatalogItem {
		decodeCount++
		var items []shared.EquipmentCatalogItem
		if json.Unmarshal(data, &items) != nil {
			return fallback
		}
		return items
	}
	for i := 0; i < 3; i++ {
		items := cache.load(path, nil, decode)
		if len(items) != 1 || items[0].ID != 1001 {
			t.Fatalf("load %d items = %+v", i, items)
		}
	}
	if decodeCount != 1 {
		t.Fatalf("decode count = %d, want 1", decodeCount)
	}
}

func TestEquipmentCacheRefreshesOnSizeOrModTimeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvf_equipment_catalog.json")
	fixed := time.Now().Add(-time.Hour).Truncate(time.Second)

	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 1, Name: "a"}})
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if items := Equipment(dir); len(items) != 1 || items[0].ID != 1 {
		t.Fatalf("initial equipment = %+v", items)
	}

	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 22, Name: "longer"}})
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if firstInfo.Size() == secondInfo.Size() || !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Fatalf("size refresh fixture has first=%d/%s second=%d/%s", firstInfo.Size(), firstInfo.ModTime(), secondInfo.Size(), secondInfo.ModTime())
	}
	if items := Equipment(dir); len(items) != 1 || items[0].ID != 22 {
		t.Fatalf("size-refreshed equipment = %+v", items)
	}

	future := fixed.Add(2 * time.Hour)
	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 33, Name: "longer"}})
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	thirdInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if secondInfo.Size() != thirdInfo.Size() || secondInfo.ModTime().Equal(thirdInfo.ModTime()) {
		t.Fatalf("mtime refresh fixture has second=%d/%s third=%d/%s", secondInfo.Size(), secondInfo.ModTime(), thirdInfo.Size(), thirdInfo.ModTime())
	}
	if items := Equipment(dir); len(items) != 1 || items[0].ID != 33 {
		t.Fatalf("mtime-refreshed equipment = %+v", items)
	}
}

func TestItemCatalogSnapshotReturnsDeepCopies(t *testing.T) {
	dir := t.TempDir()
	trade := true
	writeCatalogJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []shared.EquipmentCatalogItem{{
		ID: 1001, UseJob: []int{1, 2}, CanTrade: &trade,
	}})
	writeCatalogJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []shared.EquipmentCatalogItem{{
		ID: 2001, UseJob: []int{3}, CanTrade: &trade,
	}})

	first := ItemCatalogs(dir)
	first.Equipment[0].ID = 9999
	first.Equipment[0].UseJob[0] = 99
	*first.Equipment[0].CanTrade = false
	first.Stackable[0].UseJob[0] = 88
	*first.Stackable[0].CanTrade = false

	second := ItemCatalogs(dir)
	if got := second.Equipment[0]; got.ID != 1001 || len(got.UseJob) != 2 || got.UseJob[0] != 1 || got.CanTrade == nil || !*got.CanTrade {
		t.Fatalf("equipment cache was mutated: %+v", got)
	}
	if got := second.Stackable[0]; got.ID != 2001 || len(got.UseJob) != 1 || got.UseJob[0] != 3 || got.CanTrade == nil || !*got.CanTrade {
		t.Fatalf("stackable cache was mutated: %+v", got)
	}
}

func TestItemCatalogViewRefreshesWithoutCopyingUnchangedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvf_stackable_catalog.json")
	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 2001}})

	first := ViewStackable(dir)
	second := ViewStackable(dir)
	if len(first) != 1 || len(second) != 1 || &first[0] != &second[0] {
		t.Fatalf("unchanged catalog view was not reused: first=%+v second=%+v", first, second)
	}

	writeCatalogJSON(t, path, []shared.EquipmentCatalogItem{{ID: 2002}, {ID: 2003}})
	third := ViewStackable(dir)
	if len(third) != 2 || third[0].ID != 2002 || third[1].ID != 2003 {
		t.Fatalf("catalog view did not refresh: %+v", third)
	}
	if len(first) != 1 || first[0].ID != 2001 {
		t.Fatalf("previous catalog view was mutated: %+v", first)
	}
}

func BenchmarkMapsCached(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	entries := benchmarkMapEntries()
	writeCatalogJSON(b, path, entries)
	_ = Maps(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Maps(dir)
	}
}

func BenchmarkMapViewCached(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	entries := benchmarkMapEntries()
	writeCatalogJSON(b, path, entries)
	_ = ViewMaps(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ViewMaps(dir)
	}
}

func BenchmarkMapsReadAndDecode(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "pvf_map_catalog.json")
	writeCatalogJSON(b, path, benchmarkMapEntries())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var entries []shared.MapCatalogItem
		if err := readJSON(path, &entries); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShoutTemplatesCached(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "robot_shout_templates.json")
	messages := make([]string, 128)
	for i := range messages {
		messages[i] = "benchmark-message-" + strconv.Itoa(i)
	}
	writeCatalogJSON(b, path, messages)
	_ = ShoutTemplates(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ShoutTemplates(dir)
	}
}

func BenchmarkShoutTemplatesReadAndDecode(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "robot_shout_templates.json")
	messages := make([]string, 128)
	for i := range messages {
		messages[i] = "benchmark-message-" + strconv.Itoa(i)
	}
	writeCatalogJSON(b, path, messages)
	fallback := robottemplate.ShoutTemplates{Channel: "world", Type: 80, Messages: []string{"hello"}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		_ = decodeShoutTemplates(data, fallback)
	}
}

func BenchmarkItemCatalogsCached(b *testing.B) {
	dir := b.TempDir()
	items := benchmarkEquipmentEntries()
	writeCatalogJSON(b, filepath.Join(dir, "pvf_equipment_catalog.json"), items)
	writeCatalogJSON(b, filepath.Join(dir, "pvf_stackable_catalog.json"), items)
	_ = ItemCatalogs(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ItemCatalogs(dir)
	}
}

func BenchmarkItemCatalogViewCached(b *testing.B) {
	dir := b.TempDir()
	items := benchmarkEquipmentEntries()
	writeCatalogJSON(b, filepath.Join(dir, "pvf_equipment_catalog.json"), items)
	writeCatalogJSON(b, filepath.Join(dir, "pvf_stackable_catalog.json"), items)
	_ = ViewItemCatalogs(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ViewItemCatalogs(dir)
	}
}

func BenchmarkItemCatalogsReadAndDecode(b *testing.B) {
	dir := b.TempDir()
	equipmentPath := filepath.Join(dir, "pvf_equipment_catalog.json")
	stackablePath := filepath.Join(dir, "pvf_stackable_catalog.json")
	items := benchmarkEquipmentEntries()
	writeCatalogJSON(b, equipmentPath, items)
	writeCatalogJSON(b, stackablePath, items)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var equipment []shared.EquipmentCatalogItem
		if err := readJSON(equipmentPath, &equipment); err != nil {
			b.Fatal(err)
		}
		var stackable []shared.EquipmentCatalogItem
		if err := readJSON(stackablePath, &stackable); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkMapEntries() []shared.MapCatalogItem {
	entries := make([]shared.MapCatalogItem, 128)
	for i := range entries {
		entries[i] = shared.MapCatalogItem{Village: i % 8, Area: i, XMin: 100, XMax: 1800, YMin: 100, YMax: 500, Use: true}
	}
	return entries
}

func benchmarkEquipmentEntries() []shared.EquipmentCatalogItem {
	trade := true
	items := make([]shared.EquipmentCatalogItem, 2048)
	for i := range items {
		items[i] = shared.EquipmentCatalogItem{
			ID: i + 1, Name: "benchmark-item-" + strconv.Itoa(i), Level: i % 70,
			ItemType: i%29 + 1, UseJob: []int{i % 16}, CanTrade: &trade,
		}
	}
	return items
}

type testingTB interface {
	Helper()
	Fatal(...interface{})
}

func writeCatalogJSON(t testingTB, path string, value interface{}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
