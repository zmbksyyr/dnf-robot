package repository

import (
	"reflect"
	"strings"
	"testing"
)

func TestCreateCharacterStatInsertInitializesPreviousVillage(t *testing.T) {
	query, args := createCharacterStatInsert(661, 12345, 5)
	if !strings.Contains(query, "village,village_prev") {
		t.Fatalf("query does not initialize both village fields: %s", query)
	}
	want := []interface{}{661, "100", 12345, "-1", 5, 5}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}
