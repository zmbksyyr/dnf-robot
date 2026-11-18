//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

// BackgroundCommand preserves standard streams without creating a console.
func BackgroundCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x8} // DETACHED_PROCESS
	return cmd
}
