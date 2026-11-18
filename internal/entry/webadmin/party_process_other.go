//go:build !linux && !windows

package webadmin

import "fmt"

func withStoppedProcess(int, func() error) error {
	return fmt.Errorf("party compatibility patch is only supported on Linux")
}

func gamePIDForPort(port int) (int, error) { return gamePIDForPortLinux(port) }
