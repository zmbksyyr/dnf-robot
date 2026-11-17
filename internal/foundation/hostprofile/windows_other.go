//go:build !windows

package hostprofile

import (
	"fmt"
	"time"
)

func WindowsSupported() bool            { return false }
func ProcessID(string) (int, error)     { return 0, fmt.Errorf("Windows process adapter unavailable") }
func StopService(string) error          { return fmt.Errorf("Windows process adapter unavailable") }
func StartService(string, string) error { return fmt.Errorf("Windows process adapter unavailable") }
func ProcessStartTime(int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("Windows process adapter unavailable")
}
func ImageSegments(int, string) ([][2]int64, error) {
	return nil, fmt.Errorf("Windows process adapter unavailable")
}
