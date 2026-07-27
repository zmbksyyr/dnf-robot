package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/shared"
)

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

func TestNameTemplatesCacheRefreshesAndReusesUnchangedValue(t *testing.T) {
	dir := t.TempDir()
	missing := NameTemplates(dir)
	if len(missing.Prefixes) == 0 || missing.Pattern == "" {
		t.Fatalf("missing fallback = %+v", missing)
	}

	path := filepath.Join(dir, "robot_name_templates.json")
	writeCatalogJSON(t, path, []string{"first", "second"})
	loaded := NameTemplates(dir)
	if len(loaded.Names) != 2 || loaded.Names[0] != "first" {
		t.Fatalf("loaded templates = %+v", loaded)
	}
	if again := NameTemplates(dir); len(again.Names) != 2 || again.Names[0] != "first" {
		t.Fatalf("cached templates = %+v", again)
	}

	writeCatalogJSON(t, path, map[string]interface{}{
		"prefixes":   []string{"new"},
		"middles":    []string{"middle"},
		"suffixes":   []string{"suffix"},
		"pattern":    "{prefix}{number}",
		"number_min": 1,
		"number_max": 2,
	})
	refreshed := NameTemplates(dir)
	if len(refreshed.Prefixes) != 1 || refreshed.Prefixes[0] != "new" || refreshed.Pattern != "{prefix}{number}" {
		t.Fatalf("refreshed templates = %+v", refreshed)
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
	if items := ViewItemCatalogs(dir).Equipment; len(items) != 1 || items[0].ID != 1 {
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
	if items := ViewItemCatalogs(dir).Equipment; len(items) != 1 || items[0].ID != 22 {
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
	if items := ViewItemCatalogs(dir).Equipment; len(items) != 1 || items[0].ID != 33 {
		t.Fatalf("mtime-refreshed equipment = %+v", items)
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

func BenchmarkNameTemplatesCached(b *testing.B) {
	dir := b.TempDir()
	writeCatalogJSON(b, filepath.Join(dir, "robot_name_templates.json"), robottemplate.NameTemplates{
		Prefixes:  []string{"Alpha", "Beta", "Gamma", "Delta"},
		Middles:   []string{"Blade", "Wind", "Light", "Fire"},
		Suffixes:  []string{"One", "Two", "X", "Z"},
		Pattern:   "{prefix}{middle}{suffix}{number}",
		NumberMin: 10,
		NumberMax: 99,
	})
	_ = NameTemplates(dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NameTemplates(dir)
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
