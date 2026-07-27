//go:build linux

package webadmin

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"time"
)

func withStoppedProcess(pid int, fn func() error) (err error) {
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return err
	}
	defer func() {
		if resumeErr := syscall.Kill(pid, syscall.SIGCONT); err == nil && resumeErr != nil {
			err = resumeErr
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte("State:\tT")) || bytes.Contains(data, []byte("State:\tt")) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for pid %d to stop", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fn()
}
