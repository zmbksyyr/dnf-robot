package process

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"robot/internal/foundation/lockhub"
)

const resourceSnapshotTTL = time.Second

type resourceValues struct {
	cpuPercent float64
	memoryMB   int
	goroutines int
}

type resourceSnapshotCache struct {
	lockhub.Locker
	values resourceValues
	at     time.Time
}

var processResourceCache resourceSnapshotCache

func ResourceSnapshot() (float64, int, int) {
	values := processResourceCache.load(time.Now(), collectResourceSnapshot)
	return values.cpuPercent, values.memoryMB, values.goroutines
}

func (c *resourceSnapshotCache) load(now time.Time, collect func() resourceValues) resourceValues {
	c.Lock()
	defer c.Unlock()
	if !c.at.IsZero() && now.Sub(c.at) < resourceSnapshotTTL {
		return c.values
	}
	c.values = collect()
	c.at = now
	return c.values
}

func collectResourceSnapshot() resourceValues {
	memMB := linuxProcessRSSMB()
	if memMB <= 0 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		memMB = int(ms.Sys / 1024 / 1024)
	}
	return resourceValues{
		cpuPercent: linuxProcessCPUPercent(),
		memoryMB:   memMB,
		goroutines: runtime.NumGoroutine(),
	}
}

var cpuSample struct {
	lockhub.Locker
	ticks float64
	at    time.Time
}

func linuxProcessCPUPercent() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(stat))
	if len(parts) < 15 {
		return 0
	}
	utime, err1 := strconv.ParseFloat(parts[13], 64)
	stime, err2 := strconv.ParseFloat(parts[14], 64)
	if err1 != nil || err2 != nil {
		return 0
	}
	const clockTicks = 100.0
	now := time.Now()
	ticks := utime + stime
	cpuSample.Lock()
	defer cpuSample.Unlock()
	if cpuSample.at.IsZero() {
		cpuSample.ticks = ticks
		cpuSample.at = now
		return 0
	}
	elapsed := now.Sub(cpuSample.at).Seconds()
	deltaTicks := ticks - cpuSample.ticks
	cpuSample.ticks = ticks
	cpuSample.at = now
	if elapsed <= 0 || deltaTicks < 0 {
		return 0
	}
	return (deltaTicks / clockTicks) / elapsed * 100
}

func linuxProcessRSSMB() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(data))
	if len(parts) < 2 {
		return 0
	}
	pages, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return pages * os.Getpagesize() / 1024 / 1024
}
