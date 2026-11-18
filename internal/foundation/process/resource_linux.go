//go:build linux

package process

import (
	"os"
	"strconv"
	"strings"
	"time"

	"robot/internal/foundation/lockhub"
)

var linuxCPUSample struct {
	lockhub.Locker
	ticks float64
	at    time.Time
}

func processCPUPercent() float64 {
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
	linuxCPUSample.Lock()
	defer linuxCPUSample.Unlock()
	if linuxCPUSample.at.IsZero() {
		linuxCPUSample.ticks = ticks
		linuxCPUSample.at = now
		return 0
	}
	elapsed := now.Sub(linuxCPUSample.at).Seconds()
	deltaTicks := ticks - linuxCPUSample.ticks
	linuxCPUSample.ticks = ticks
	linuxCPUSample.at = now
	if elapsed <= 0 || deltaTicks < 0 {
		return 0
	}
	return (deltaTicks / clockTicks) / elapsed * 100
}

func processRSSMB() int {
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
