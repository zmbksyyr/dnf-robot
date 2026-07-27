//go:build linux

package process

import (
	"errors"
	"syscall"
)

type systemFileLimitAPI struct{}

func (systemFileLimitAPI) get() (fileLimit, error) {
	var limit syscall.Rlimit
	if err := getRlimitCompat(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return fileLimit{}, err
	}
	return fileLimit{soft: limit.Cur, hard: limit.Max}, nil
}

func (systemFileLimitAPI) set(limit fileLimit) error {
	return setRlimitCompat(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: limit.soft, Max: limit.hard})
}

func getRlimitCompat(resource int, limit *syscall.Rlimit) error {
	err := syscall.Getrlimit(resource, limit)
	if !errors.Is(err, syscall.ENOSYS) {
		return err
	}
	// Go implements Getrlimit with prlimit64, which was added in Linux 2.6.36.
	// CentOS 5 and early CentOS 6 kernels need the original getrlimit syscall.
	return kernelGetrlimit(resource, limit)
}

func setRlimitCompat(resource int, limit *syscall.Rlimit) error {
	err := syscall.Setrlimit(resource, limit)
	if !errors.Is(err, syscall.ENOSYS) {
		return err
	}
	return kernelSetrlimit(resource, limit)
}

// EnsureOpenFileLimit raises the Linux descriptor limit when the configured
// robot capacity would otherwise exceed it.
func EnsureOpenFileLimit(maxOnlineRobots, dbMaxConnections int) error {
	return ensureOpenFileLimit(systemFileLimitAPI{}, maxOnlineRobots, dbMaxConnections)
}
