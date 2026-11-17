//go:build !windows

package process

import "errors"

var ErrStopEventUnsupported = errors.New("named stop events are only supported on Windows")

func WaitForStopEvent(_ string) (<-chan struct{}, error) {
	return nil, ErrStopEventUnsupported
}

func SignalReadyEvent(_ string) error { return ErrStopEventUnsupported }
