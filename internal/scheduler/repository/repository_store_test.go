package repository

import (
	"reflect"
	"strings"
	"testing"

	storecap "robot/internal/capability/store"
)

func TestBuildStoreStallInsertBatchesRows(t *testing.T) {
	items := []storecap.StallItem{
		{ItemID: 3037, Price: 100001, Count: 500},
		{ItemID: 3031, Price: 100002, Count: 600},
	}
	query, args := buildStoreStallInsert(17000001, items)
	if got := strings.Count(query, "(?,?,?,?,1,?)"); got != len(items) {
		t.Fatalf("value groups = %d, want %d: %s", got, len(items), query)
	}
	wantArgs := []interface{}{3037, 100001, 500, 2, 17000001, 3031, 100002, 600, 2, 17000001}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("insert args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuildStoreStallInsertSkipsEmptyInput(t *testing.T) {
	if query, args := buildStoreStallInsert(17000001, nil); query != "" || args != nil {
		t.Fatalf("empty insert = %q %#v", query, args)
	}
}

func TestStorePermissionReadyRequiresEveryRecord(t *testing.T) {
	complete := storecap.PermissionStatus{Premium: 1, Miles: 1, ProdUser: 1, PUUser: 1, EventEntry: 1}
	if !storePermissionReady(complete) {
		t.Fatal("complete permission was rejected")
	}
	complete.EventEntry = 0
	if storePermissionReady(complete) {
		t.Fatal("incomplete permission was accepted")
	}
}
