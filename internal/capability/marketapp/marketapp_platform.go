package marketapp

import (
	"fmt"
	"runtime"

	"robot/internal/foundation/hostprofile"
)

// Linux keeps its original process implementation. Windows requires an
// explicit host profile before any process or memory operation is offered.
func marketPlatformError() error {
	if runtime.GOOS != "linux" {
		if hostprofile.WindowsSupported() {
			return nil
		}
		return fmt.Errorf("host adapter %q has no complete Windows service contract", hostprofile.Current().Adapter)
	}
	return nil
}

func marketMemoryPlatformError() error {
	if runtime.GOOS != "linux" {
		if hostprofile.WindowsSupported() && hostprofile.Current().Capabilities["memory_patch"] {
			return nil
		}
		return fmt.Errorf("host profile does not support market process memory patches")
	}
	return nil
}

func marketOwnedByController() bool {
	return hostprofile.Current().MarketOwner == "controller"
}
