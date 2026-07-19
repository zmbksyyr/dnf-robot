//go:build linux

package process

import "syscall"

type systemFileLimitAPI struct{}

func (systemFileLimitAPI) get() (fileLimit, error) {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return fileLimit{}, err
	}
	return fileLimit{soft: limit.Cur, hard: limit.Max}, nil
}

func (systemFileLimitAPI) set(limit fileLimit) error {
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: limit.soft, Max: limit.hard})
}

// EnsureOpenFileLimit raises the Linux descriptor limit when the configured
// robot capacity would otherwise exceed it.
func EnsureOpenFileLimit(maxOnlineRobots, dbMaxConnections int) error {
	return ensureOpenFileLimit(systemFileLimitAPI{}, maxOnlineRobots, dbMaxConnections)
}
