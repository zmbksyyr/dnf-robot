//go:build windows

package webadmin

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"robot/internal/foundation/hostprofile"
)

func gamePIDForPort(port int) (int, error) {
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid game port %d", port)
	}
	data, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return 0, fmt.Errorf("read listening ports: %w", err)
	}
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") ||
			!strings.EqualFold(fields[3], "LISTENING") || !strings.HasSuffix(fields[1], suffix) {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[4])
		if parseErr != nil || pid <= 0 {
			continue
		}
		verified, verifyErr := hostprofile.ProcessID("game")
		if verifyErr != nil {
			return 0, verifyErr
		}
		if verified != pid {
			return 0, fmt.Errorf("port %d belongs to another game process", port)
		}
		return pid, nil
	}
	return 0, fmt.Errorf("%w on port %d", errPartyCompatUnavailable, port)
}

const (
	threadSuspendResume = 0x0002
	snapshotThreads     = 0x00000004
	invalidHandleValue  = ^uintptr(0)
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	thread32First            = kernel32.NewProc("Thread32First")
	thread32Next             = kernel32.NewProc("Thread32Next")
	openThread               = kernel32.NewProc("OpenThread")
	suspendThread            = kernel32.NewProc("SuspendThread")
	resumeThread             = kernel32.NewProc("ResumeThread")
	closeHandle              = kernel32.NewProc("CloseHandle")
)

type windowsThreadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}

// withStoppedProcess applies a memory patch while every thread belonging to
// the verified game host is suspended. Windows has no SIGSTOP equivalent;
// suspending the process through its threads gives the same atomic patching
// boundary without attaching a debugger or changing global process state.
func withStoppedProcess(pid int, fn func() error) (err error) {
	if pid <= 0 {
		return fmt.Errorf("invalid process pid %d", pid)
	}
	handles, err := suspendProcessThreads(uint32(pid))
	if err != nil {
		return err
	}
	defer func() {
		if resumeErr := resumeProcessThreads(handles); err == nil && resumeErr != nil {
			err = resumeErr
		}
	}()
	if fn == nil {
		return nil
	}
	return fn()
}

func suspendProcessThreads(pid uint32) ([]syscall.Handle, error) {
	snapshot, _, callErr := createToolhelp32Snapshot.Call(snapshotThreads, 0)
	if snapshot == invalidHandleValue {
		return nil, fmt.Errorf("create thread snapshot: %w", callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))

	entry := windowsThreadEntry32{Size: uint32(unsafe.Sizeof(windowsThreadEntry32{}))}
	first, _, firstErr := thread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if first == 0 {
		return nil, fmt.Errorf("enumerate process threads: %w", firstErr)
	}
	handles := make([]syscall.Handle, 0, 8)
	closeOpened := func() {
		for _, handle := range handles {
			_, _, _ = resumeThread.Call(uintptr(handle))
			_, _, _ = closeHandle.Call(uintptr(handle))
		}
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, _, openErr := openThread.Call(threadSuspendResume, 0, uintptr(entry.ThreadID))
			if thread == 0 {
				closeOpened()
				return nil, fmt.Errorf("open game thread %d: %w", entry.ThreadID, openErr)
			}
			previous, _, suspendErr := suspendThread.Call(thread)
			if uint32(previous) == 0xffffffff {
				_, _, _ = closeHandle.Call(thread)
				closeOpened()
				return nil, fmt.Errorf("suspend game thread %d: %w", entry.ThreadID, suspendErr)
			}
			handles = append(handles, syscall.Handle(thread))
		}
		next, _, nextErr := thread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if next == 0 {
			if nextErr != syscall.Errno(18) {
				closeOpened()
				return nil, fmt.Errorf("enumerate game threads: %w", nextErr)
			}
			if len(handles) == 0 {
				return nil, fmt.Errorf("no threads found for process %d: %w", pid, nextErr)
			}
			break
		}
	}
	return handles, nil
}

func resumeProcessThreads(handles []syscall.Handle) error {
	var firstErr error
	for _, handle := range handles {
		if previous, _, err := resumeThread.Call(uintptr(handle)); uint32(previous) == 0xffffffff && firstErr == nil {
			firstErr = fmt.Errorf("resume game thread: %w", err)
		}
		_, _, _ = closeHandle.Call(uintptr(handle))
	}
	return firstErr
}
