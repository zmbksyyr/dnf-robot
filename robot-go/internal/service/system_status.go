package service

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SystemStatus struct {
	UpdatedAt       time.Time `json:"updated_at"`
	RobotCPUPercent float64   `json:"robot_cpu_percent"`
	RobotMemoryMB   int       `json:"robot_memory_mb"`
	RobotThreads    int       `json:"robot_threads"`
	RobotUptimeSec  int       `json:"robot_uptime_seconds"`
	Running         int       `json:"running"`
	Store           int       `json:"store"`
}

func (m *RobotManager) SystemStatus() SystemStatus {
	auto := m.AutoStatus()
	cpu, mem, threads := robotResourceSnapshot()
	return SystemStatus{
		UpdatedAt:       time.Now(),
		RobotCPUPercent: cpu,
		RobotMemoryMB:   mem,
		RobotThreads:    threads,
		RobotUptimeSec:  int(time.Since(m.startedAt).Seconds()),
		Running:         auto.Running,
		Store:           auto.StoreRunning,
	}
}

func robotResourceSnapshot() (float64, int, int) {
	memMB := linuxProcessRSSMB()
	if memMB <= 0 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		memMB = int(ms.Sys / 1024 / 1024)
	}
	return linuxProcessCPUPercent(), memMB, runtime.NumGoroutine()
}

var processCPUSample struct {
	sync.Mutex
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
	processCPUSample.Lock()
	defer processCPUSample.Unlock()
	if processCPUSample.at.IsZero() {
		processCPUSample.ticks = ticks
		processCPUSample.at = now
		return 0
	}
	elapsed := now.Sub(processCPUSample.at).Seconds()
	deltaTicks := ticks - processCPUSample.ticks
	processCPUSample.ticks = ticks
	processCPUSample.at = now
	if elapsed <= 0 || deltaTicks < 0 {
		return 0
	}
	percent := (deltaTicks / clockTicks) / elapsed * 100
	if percent < 0 {
		return 0
	}
	return percent
}

func linuxProcessRSSMB() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
