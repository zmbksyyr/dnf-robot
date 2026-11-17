//go:build windows

package webadmin

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// Windows has no SIGTERM for child processes. Use the same standalone named
// event option as the main process, and cancel the HTTP server's own context.
func prepareWebShutdown(cmd *exec.Cmd) (func() error, func(), error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	name := fmt.Sprintf("Local\\robot-web-stop-%d-%d", os.Getpid(), time.Now().UnixNano())
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, err
	}
	h, _, err := kernel.NewProc("CreateEventW").Call(0, 1, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return nil, nil, err
	}
	cmd.Args = append(cmd.Args, "--stop-event", name)
	return func() error {
		ok, _, err := kernel.NewProc("SetEvent").Call(h)
		if ok == 0 {
			return err
		}
		return nil
	}, func() { _ = syscall.CloseHandle(syscall.Handle(h)) }, nil
}
