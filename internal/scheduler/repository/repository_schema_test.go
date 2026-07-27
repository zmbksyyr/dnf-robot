package repository

import (
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEnsureSchemaCachesOnlySuccess(t *testing.T) {
	repository := &SQLRepository{}
	calls := 0
	fail := true
	exec := func(string, ...interface{}) (sql.Result, error) {
		calls++
		if fail && calls == 3 {
			return nil, errors.New("schema unavailable")
		}
		return nil, nil
	}

	if err := repository.ensureSchema(exec); err == nil {
		t.Fatal("first ensure unexpectedly succeeded")
	}
	if calls != 3 {
		t.Fatalf("calls after failure got %d want 3", calls)
	}

	fail = false
	if err := repository.ensureSchema(exec); err != nil {
		t.Fatalf("retry ensure: %v", err)
	}
	wantCalls := 3 + len(schemaStatements)
	if calls != wantCalls {
		t.Fatalf("calls after retry got %d want %d", calls, wantCalls)
	}
	if err := repository.ensureSchema(func(string, ...interface{}) (sql.Result, error) {
		t.Fatal("cached ensure executed another statement")
		return nil, nil
	}); err != nil {
		t.Fatalf("cached ensure: %v", err)
	}
}

func TestEnsureSchemaConcurrentCallsShareSuccess(t *testing.T) {
	repository := &SQLRepository{}
	var calls atomic.Int32
	exec := func(string, ...interface{}) (sql.Result, error) {
		calls.Add(1)
		return nil, nil
	}

	const callers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := repository.ensureSchema(exec); err != nil {
				t.Errorf("ensure schema: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := int(calls.Load()); got != len(schemaStatements) {
		t.Fatalf("statement calls got %d want %d", got, len(schemaStatements))
	}
}
