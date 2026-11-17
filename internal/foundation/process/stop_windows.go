//go:build windows

package process

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	openEvent           = kernel32.NewProc("OpenEventW")
	waitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	closeHandle         = kernel32.NewProc("CloseHandle")
	setEvent            = kernel32.NewProc("SetEvent")
)

const (
	eventModifyState = 0x0002
	synchronize      = 0x00100000
	waitObject0      = 0
	waitFailed       = 0xffffffff
)

func WaitForStopEvent(name string) (<-chan struct{}, error) {
	if name == "" {
		return nil, fmt.Errorf("empty stop event name")
	}
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, _, callErr := openEvent.Call(synchronize, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return nil, fmt.Errorf("open stop event %q: %w", name, callErr)
	}
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		defer closeHandle.Call(h)
		result, _, _ := waitForSingleObject.Call(h, 0xffffffff)
		if result == waitObject0 || result == waitFailed {
			return
		}
	}()
	return ch, nil
}

func SignalReadyEvent(name string) error {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	h, _, openErr := openEvent.Call(eventModifyState, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return fmt.Errorf("open ready event: %w", openErr)
	}
	defer closeHandle.Call(h)
	ok, _, callErr := setEvent.Call(h)
	if ok == 0 {
		return fmt.Errorf("signal ready event: %w", callErr)
	}
	return nil
}
