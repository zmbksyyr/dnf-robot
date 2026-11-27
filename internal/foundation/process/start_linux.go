//go:build linux

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
)

// StartWithMemoryPatch stops the child at exec, before its first instruction.
// Only a successfully patched child is detached and allowed to run. The caller
// must reap a successful command with Wait.
func StartWithMemoryPatch(cmd *exec.Cmd, patch func(int, MemoryFile) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	attr.Ptrace = true
	cmd.SysProcAttr = &attr
	if err := cmd.Start(); err != nil {
		return err
	}
	running := false
	defer func() {
		if !running {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	pid := cmd.Process.Pid
	var status syscall.WaitStatus
	for {
		_, err := syscall.Wait4(pid, &status, 0, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("wait for exec pid %d: %w", pid, err)
		}
		break
	}
	if !status.Stopped() || status.StopSignal() != syscall.SIGTRAP {
		return fmt.Errorf("pid %d did not stop at exec: %v", pid, status)
	}
	mem, err := openMemoryFile(pid, true)
	if err != nil {
		return err
	}
	err = patch(pid, &processMemoryFile{file: mem, pid: pid, traced: true})
	_ = mem.Close()
	if err != nil {
		return err
	}
	if err := syscall.PtraceDetach(pid); err != nil {
		return fmt.Errorf("detach patched pid %d: %w", pid, err)
	}
	running = true
	return nil
}
