//go:build linux && amd64

package process

import (
	"syscall"
	"unsafe"
)

func kernelGetrlimit(resource int, limit *syscall.Rlimit) error {
	_, _, errno := syscall.RawSyscall(
		syscall.SYS_GETRLIMIT,
		uintptr(resource),
		uintptr(unsafe.Pointer(limit)),
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func kernelSetrlimit(resource int, limit *syscall.Rlimit) error {
	_, _, errno := syscall.RawSyscall(
		syscall.SYS_SETRLIMIT,
		uintptr(resource),
		uintptr(unsafe.Pointer(limit)),
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
