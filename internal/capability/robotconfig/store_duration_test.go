package robotconfig

import (
	"testing"
	"time"
)

func TestStoreDurationForUIDIsDeterministicAndBounded(t *testing.T) {
	const base = 210
	minDuration := time.Duration(base-35) * time.Second
	maxDuration := time.Duration(base+35) * time.Second
	first := StoreDurationForUID(base, 17000123)
	if second := StoreDurationForUID(base, 17000123); second != first {
		t.Fatalf("duration changed for the same uid: first=%s second=%s", first, second)
	}
	if first < minDuration || first > maxDuration {
		t.Fatalf("duration = %s, want %s..%s", first, minDuration, maxDuration)
	}
}

func TestStoreDurationForUIDSpreadsAroundConfiguredAverage(t *testing.T) {
	const (
		base  = 210
		count = 7100
	)
	var total time.Duration
	seen := make(map[time.Duration]bool)
	for uid := 17000000; uid < 17000000+count; uid++ {
		duration := StoreDurationForUID(base, uid)
		if duration < 175*time.Second || duration > 245*time.Second {
			t.Fatalf("uid=%d duration=%s outside expected bounds", uid, duration)
		}
		total += duration
		seen[duration] = true
	}
	average := total / count
	if delta := average - base*time.Second; delta < -time.Second || delta > time.Second {
		t.Fatalf("average duration = %s, want within 1s of %ds", average, base)
	}
	if len(seen) < 60 {
		t.Fatalf("duration spread has only %d distinct values", len(seen))
	}
}

func TestStoreDurationJitterScalesForShortDurations(t *testing.T) {
	if got := StoreDurationJitterSec(60); got != 10 {
		t.Fatalf("jitter for 60s = %d, want 10", got)
	}
	if got := MaxStoreDurationSec(60); got != 70 {
		t.Fatalf("max duration for 60s = %d, want 70", got)
	}
	if got := StoreDurationForUID(0, 100); got != 0 {
		t.Fatalf("zero configured duration = %s, want 0", got)
	}
}
