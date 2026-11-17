package marketapp

import (
	"fmt"
	"runtime"

	"robot/internal/foundation/hostprofile"
)

// marketPlatformError is intentionally small and shared by every operation
// that needs Linux process discovery or /proc memory access. Windows Robot is
// still useful for its API, database and PVF features, but it must not report
// the Linux-only market maintenance as completed.
func marketPlatformError() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("market process and memory maintenance requires Linux; host adapter %q is not supported", hostprofile.Current().Adapter)
	}
	return nil
}

func marketOwnedByController() bool {
	return hostprofile.Current().MarketOwner == "controller"
}
