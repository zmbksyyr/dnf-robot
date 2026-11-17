//go:build !windows

package webadmin

import (
	"os/exec"
	"syscall"
)

func prepareWebShutdown(cmd *exec.Cmd) (func() error, func(), error) {
	return func() error { return cmd.Process.Signal(syscall.SIGTERM) }, func() {}, nil
}
