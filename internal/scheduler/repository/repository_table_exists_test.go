package repository

import (
	"errors"
	"testing"
)

func TestCachedTableExistsCachesPositiveAndNegativeResults(t *testing.T) {
	repository := &SQLRepository{}
	calls := 0
	load := func() (bool, error) {
		calls++
		return true, nil
	}

	for i := 0; i < 2; i++ {
		exists, err := repository.cachedTableExists("db.present", load)
		if err != nil || !exists {
			t.Fatalf("present table result exists=%v err=%v", exists, err)
		}
	}
	if calls != 1 {
		t.Fatalf("present table loads got %d want 1", calls)
	}

	missingCalls := 0
	missing := func() (bool, error) {
		missingCalls++
		return false, nil
	}
	for i := 0; i < 2; i++ {
		exists, err := repository.cachedTableExists("db.missing", missing)
		if err != nil || exists {
			t.Fatalf("missing table result exists=%v err=%v", exists, err)
		}
	}
	if missingCalls != 1 {
		t.Fatalf("missing table loads got %d want 1", missingCalls)
	}
}

func TestCachedTableExistsRetriesFailureAndSupportsInvalidation(t *testing.T) {
	repository := &SQLRepository{}
	calls := 0
	load := func() (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("schema query failed")
		}
		return true, nil
	}

	if _, err := repository.cachedTableExists("db.table", load); err == nil {
		t.Fatal("first table load unexpectedly succeeded")
	}
	if exists, err := repository.cachedTableExists("db.table", load); err != nil || !exists {
		t.Fatalf("table load retry exists=%v err=%v", exists, err)
	}
	if _, err := repository.cachedTableExists("db.table", load); err != nil {
		t.Fatalf("cached table load: %v", err)
	}
	if calls != 2 {
		t.Fatalf("table loads got %d want 2", calls)
	}

	repository.invalidateTableExists("db.table")
	if _, err := repository.cachedTableExists("db.table", load); err != nil {
		t.Fatalf("table load after invalidation: %v", err)
	}
	if calls != 3 {
		t.Fatalf("table loads after invalidation got %d want 3", calls)
	}
}
