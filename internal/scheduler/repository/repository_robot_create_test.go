package repository

import (
	"reflect"
	"strings"
	"testing"

	robotcap "robot/internal/capability/robot"
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

func TestCreateCharacterInfoInsertSuppliesStrictSchemaFields(t *testing.T) {
	info := robotcap.Info{UID: 17000508, CID: 31, Village: 1, Level: 70, Job: 0, Grow: 0}
	query, args := createCharacterInfoInsert(info, 12345, "robot", map[string]bool{
		"element_resist": true,
		"spec_property":  true,
		"VIP":            true,
		"create_time":    true,
	})
	for _, field := range []string{"element_resist", "spec_property", "VIP", "create_time"} {
		if !strings.Contains(query, "`"+field+"`") {
			t.Fatalf("query does not include %s: %s", field, query)
		}
	}
	if got := strings.Count(query, "?"); got != len(args) {
		t.Fatalf("placeholder count=%d args=%d", got, len(args))
	}
	if got, ok := args[24].([]byte); !ok || len(got) != 8 {
		t.Fatalf("element_resist arg=%#v", args[24])
	}
	if got, ok := args[25].([]byte); !ok || len(got) != 34 {
		t.Fatalf("spec_property arg=%#v", args[25])
	}
}

func TestCreateCharacterInfoInsertOmitsUnavailableStrictFields(t *testing.T) {
	info := robotcap.Info{UID: 17000508, CID: 31, Village: 1, Level: 70}
	query, args := createCharacterInfoInsert(info, 12345, "robot", map[string]bool{})
	for _, field := range []string{"element_resist", "spec_property", "VIP", "create_time"} {
		if strings.Contains(query, "`"+field+"`") {
			t.Fatalf("query unexpectedly includes %s: %s", field, query)
		}
	}
	if got := strings.Count(query, "?"); got != len(args) {
		t.Fatalf("placeholder count=%d args=%d", got, len(args))
	}
}
