package store

import (
	"reflect"
	"testing"

	"robot/internal/shared"
)

func TestFilterEligibleMapsDoesNotMutateSharedCatalog(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 1, Area: 1, Use: true},
		{Village: 1, Area: 0, Use: true},
		{Village: 2, Area: 0, Use: false},
		{Village: 2, Area: 1, Use: true},
	}
	wantOriginal := append([]shared.MapCatalogItem(nil), maps...)

	filtered := filterEligibleMaps(maps)
	if want := []shared.MapCatalogItem{maps[1], maps[3]}; !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered maps = %+v, want %+v", filtered, want)
	}
	if !reflect.DeepEqual(maps, wantOriginal) {
		t.Fatalf("shared map catalog was mutated: got %+v, want %+v", maps, wantOriginal)
	}

	filtered[0].Area = 8
	if !reflect.DeepEqual(maps, wantOriginal) {
		t.Fatalf("filtered maps still alias shared catalog: got %+v, want %+v", maps, wantOriginal)
	}
}
