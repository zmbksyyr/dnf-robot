//go:build !windows

package process

import "os/exec"

// BackgroundCommand keeps the existing process behavior on other platforms.
func BackgroundCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
