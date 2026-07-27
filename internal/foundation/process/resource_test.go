package process

import (
	"testing"
	"time"
)

func TestResourceSnapshotCacheSharesSamplingWindow(t *testing.T) {
	var cache resourceSnapshotCache
	now := time.Unix(1_700_000_000, 0)
	calls := 0
	collect := func() resourceValues {
		calls++
		return resourceValues{cpuPercent: float64(calls), memoryMB: calls, goroutines: calls}
	}

	first := cache.load(now, collect)
	second := cache.load(now.Add(resourceSnapshotTTL-time.Millisecond), collect)
	if calls != 1 || first != second {
		t.Fatalf("cached samples calls=%d first=%+v second=%+v", calls, first, second)
	}
	third := cache.load(now.Add(resourceSnapshotTTL), collect)
	if calls != 2 || third == second {
		t.Fatalf("refreshed sample calls=%d second=%+v third=%+v", calls, second, third)
	}
}
