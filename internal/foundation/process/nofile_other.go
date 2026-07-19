//go:build !linux

package process

// EnsureOpenFileLimit validates the capacity inputs on platforms without
// RLIMIT_NOFILE. Linux performs the process limit check and adjustment.
func EnsureOpenFileLimit(maxOnlineRobots, dbMaxConnections int) error {
	_, err := RequiredOpenFiles(maxOnlineRobots, dbMaxConnections)
	return err
}
