package filewatch

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollerReadsOnlyAfterStampChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int32
	poller := New(time.Hour, []Entry{{
		Name: "settings", Path: path,
		Apply: func(string) error {
			applied.Add(1)
			return nil
		},
	}}, nil)
	poller.CheckNow()
	poller.CheckNow()
	if got := applied.Load(); got != 1 {
		t.Fatalf("apply count=%d, want 1", got)
	}

	if err := os.WriteFile(path, []byte("two-two"), 0644); err != nil {
		t.Fatal(err)
	}
	poller.CheckNow()
	if got := applied.Load(); got != 2 {
		t.Fatalf("apply count=%d after change, want 2", got)
	}
}

func TestPollerRetriesRejectedStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("bad"), 0644); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	var failures atomic.Int32
	poller := New(time.Hour, []Entry{{
		Name: "settings", Path: path,
		Apply: func(string) error {
			attempts.Add(1)
			return errors.New("invalid settings")
		},
	}}, func(Entry, error) { failures.Add(1) })
	poller.CheckNow()
	poller.CheckNow()
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts=%d, want 2", got)
	}
	if got := failures.Load(); got != 2 {
		t.Fatalf("failures=%d, want 2", got)
	}
}

func TestPollerDetectsContentChangeWithSameSizeAndModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.WriteFile(path, []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int32
	poller := New(time.Hour, []Entry{{Path: path, Apply: func(string) error {
		applied.Add(1)
		return nil
	}}}, nil)
	poller.CheckNow()
	if err := os.WriteFile(path, []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	poller.CheckNow()
	if got := applied.Load(); got != 2 {
		t.Fatalf("apply count=%d, want 2", got)
	}
}

func TestPollerWakeAppliesChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	applied := make(chan struct{}, 2)
	poller := New(time.Hour, []Entry{{
		Name: "settings", Path: path,
		Apply: func(string) error {
			applied <- struct{}{}
			return nil
		},
	}}, nil)
	poller.Start()
	t.Cleanup(poller.Close)
	select {
	case <-applied:
	case <-time.After(time.Second):
		t.Fatal("initial apply timed out")
	}
	if err := os.WriteFile(path, []byte("changed-content"), 0644); err != nil {
		t.Fatal(err)
	}
	poller.Wake()
	select {
	case <-applied:
	case <-time.After(time.Second):
		t.Fatal("wake apply timed out")
	}
}

func TestPollerContainsApplyAndErrorHandlerPanics(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := os.WriteFile(first, []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int32
	poller := New(time.Hour, []Entry{
		{Name: "panic", Path: first, Apply: func(string) error { panic("bad apply") }},
		{Name: "healthy", Path: second, Apply: func(string) error { applied.Add(1); return nil }},
	}, func(Entry, error) { panic("bad error handler") })
	poller.CheckNow()
	if got := applied.Load(); got != 1 {
		t.Fatalf("healthy entry apply count = %d, want 1", got)
	}
}
