package robotconfig

import "time"

const maxStoreDurationJitterSec = 35

// StoreDurationForUID spreads store expiry around the configured duration so
// a burst of successful stores does not become an equally large expiry burst.
func StoreDurationForUID(durationSec, uid int) time.Duration {
	if durationSec <= 0 {
		return 0
	}
	jitter := StoreDurationJitterSec(durationSec)
	if jitter == 0 || uid <= 0 {
		return time.Duration(durationSec) * time.Second
	}
	span := uint64(jitter*2 + 1)
	offset := int(mixStoreDurationUID(uint64(uid))%span) - jitter
	return time.Duration(durationSec+offset) * time.Second
}

func StoreDurationJitterSec(durationSec int) int {
	if durationSec <= 1 {
		return 0
	}
	jitter := durationSec / 6
	if jitter > maxStoreDurationJitterSec {
		return maxStoreDurationJitterSec
	}
	return jitter
}

func MaxStoreDurationSec(durationSec int) int {
	if durationSec <= 0 {
		return 0
	}
	return durationSec + StoreDurationJitterSec(durationSec)
}

func mixStoreDurationUID(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ (value >> 31)
}
