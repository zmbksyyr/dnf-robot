//go:build windows

package process

import (
	"syscall"
	"time"
	"unsafe"

	"robot/internal/foundation/lockhub"
)

var (
	resourceKernel32 = syscall.NewLazyDLL("kernel32.dll")
	getCurrentProc   = resourceKernel32.NewProc("GetCurrentProcess")
	getProcessTimes  = resourceKernel32.NewProc("GetProcessTimes")
	resourcePSAPI    = syscall.NewLazyDLL("psapi.dll")
	getProcessMemory = resourcePSAPI.NewProc("GetProcessMemoryInfo")
)

type windowsResourceFileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type windowsProcessMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var windowsCPUSample struct {
	lockhub.Locker
	value uint64
	at    time.Time
}

func processRSSMB() int {
	handle, _, _ := getCurrentProc.Call()
	if handle == 0 {
		return 0
	}
	counters := windowsProcessMemoryCounters{CB: uint32(unsafe.Sizeof(windowsProcessMemoryCounters{}))}
	ok, _, _ := getProcessMemory.Call(handle, uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
	if ok == 0 {
		return 0
	}
	return int(counters.WorkingSetSize / (1024 * 1024))
}

func processCPUPercent() float64 {
	handle, _, _ := getCurrentProc.Call()
	if handle == 0 {
		return 0
	}
	var creation, exit, kernel, user windowsResourceFileTime
	ok, _, _ := getProcessTimes.Call(handle,
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0
	}
	toUint64 := func(v windowsResourceFileTime) uint64 { return uint64(v.HighDateTime)<<32 | uint64(v.LowDateTime) }
	total := toUint64(kernel) + toUint64(user)
	now := time.Now()
	windowsCPUSample.Lock()
	defer windowsCPUSample.Unlock()
	if windowsCPUSample.at.IsZero() {
		windowsCPUSample.value = total
		windowsCPUSample.at = now
		return 0
	}
	elapsed := now.Sub(windowsCPUSample.at)
	delta := total - windowsCPUSample.value
	windowsCPUSample.value = total
	windowsCPUSample.at = now
	if elapsed <= 0 {
		return 0
	}
	// FILETIME is measured in 100ns units. This reports process CPU time as a
	// percentage of one wall-clock core, matching the Linux metric.
	return float64(delta) / float64(elapsed.Nanoseconds()/100) * 100
}
