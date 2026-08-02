package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTitleCatalogSelectsStableTitleIndependentOfListOrder(t *testing.T) {
	first := NewTitleCatalog([]string{"凯丽装备铺", "平价装备店", "冒险家补给"})
	second := NewTitleCatalog([]string{"冒险家补给", "凯丽装备铺", "平价装备店"})
	for uid := 17000000; uid < 17000100; uid++ {
		want := first.TitleForUID(uid)
		if got := first.TitleForUID(uid); got != want {
			t.Fatalf("uid %d title changed between calls: %q != %q", uid, got, want)
		}
		if got := second.TitleForUID(uid); got != want {
			t.Fatalf("uid %d title changed after list reorder: %q != %q", uid, got, want)
		}
	}
}

func TestTitleCatalogFiltersInvalidValuesAndFallsBack(t *testing.T) {
	catalog := NewTitleCatalog([]string{"", "  ", "bad\ntitle", string(make([]byte, maxStoreTitleBytes+1))})
	if catalog.Len() != 0 {
		t.Fatalf("valid titles=%d, want 0", catalog.Len())
	}
	if got := catalog.TitleForUID(17000123); got != "tw-123" {
		t.Fatalf("fallback title=%q, want tw-123", got)
	}
}

func TestTitleCatalogUsesStableFallbackSlotsWhenNamesAreInsufficient(t *testing.T) {
	catalog := NewTitleCatalog([]string{"凯丽装备铺"})
	custom, fallback := 0, 0
	for uid := 17000000; uid < 17001000; uid++ {
		title := catalog.TitleForUID(uid)
		if title == "凯丽装备铺" {
			custom++
		} else {
			fallback++
			if title != fallbackStoreTitle(uid) {
				t.Fatalf("uid %d unexpected fallback title %q", uid, title)
			}
		}
	}
	if custom == 0 || fallback == 0 {
		t.Fatalf("insufficient title allocation custom=%d fallback=%d", custom, fallback)
	}
}

func TestTitleCatalogUsesCustomNamesWhenRecommendedListIsComplete(t *testing.T) {
	titles := make([]string, 0, recommendedStoreTitleCount)
	for index := 0; index < recommendedStoreTitleCount; index++ {
		titles = append(titles, fmt.Sprintf("title-%d", index))
	}
	catalog := NewTitleCatalog(titles)
	for uid := 17000000; uid < 17001000; uid++ {
		if got := catalog.TitleForUID(uid); got == fallbackStoreTitle(uid) {
			t.Fatalf("uid %d unexpectedly used fallback with a complete title list", uid)
		}
	}
}

func TestLoadTitleCatalogTrimsAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "titles.json")
	if err := os.WriteFile(path, []byte(`[" 凯丽装备铺 ","凯丽装备铺","平价装备店"]`), 0644); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadTitleCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Len() != 2 {
		t.Fatalf("valid titles=%d, want 2", catalog.Len())
	}
}
