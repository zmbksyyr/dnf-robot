//go:build !linux

package webadmin

import "fmt"

func withStoppedProcess(int, func() error) error {
	return fmt.Errorf("party compatibility patch is only supported on Linux")
}
