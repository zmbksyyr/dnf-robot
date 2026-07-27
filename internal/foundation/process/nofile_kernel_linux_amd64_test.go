//go:build linux && amd64

package process

import (
	"syscall"
	"testing"
)

func TestKernelGetrlimitMatchesPrimaryNofileLimit(t *testing.T) {
	var primary syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &primary); err != nil {
		t.Fatalf("primary Getrlimit: %v", err)
	}
	var direct syscall.Rlimit
	if err := kernelGetrlimit(syscall.RLIMIT_NOFILE, &direct); err != nil {
		t.Fatalf("kernel getrlimit: %v", err)
	}
	if direct != primary {
		t.Fatalf("direct limit = %+v, primary = %+v", direct, primary)
	}
}
