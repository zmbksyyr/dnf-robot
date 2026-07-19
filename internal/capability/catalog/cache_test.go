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

func benchmarkMapEntries() []shared.MapCatalogItem {
	entries := make([]shared.MapCatalogItem, 128)
	for i := range entries {
		entries[i] = shared.MapCatalogItem{Village: i % 8, Area: i, XMin: 100, XMax: 1800, YMin: 100, YMax: 500, Use: true}
	}
	return entries
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
