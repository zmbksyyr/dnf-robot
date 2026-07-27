//go:build linux && !amd64

package process

import "syscall"

// The deployed robot binary is linux/amd64. Keep unsupported architectures
// explicit instead of assuming that their older rlimit ABI matches amd64.
func kernelGetrlimit(_ int, _ *syscall.Rlimit) error {
	return syscall.ENOSYS
}

func kernelSetrlimit(_ int, _ *syscall.Rlimit) error {
	return syscall.ENOSYS
}
