package marketapp

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"robot/internal/foundation/hostprofile"
	"robot/internal/foundation/serviceinit"
)

const (
	defaultServiceRunScript = serviceinit.DefaultRunScript
	defaultServiceHomeRoot  = serviceinit.DefaultHomeRoot
)

type marketServiceLaunch struct {
	dir    string
	bin    string
	args   []string
	source string
}

func (a *App) discoverMarketServiceLaunch(name, preferredRoot string) (marketServiceLaunch, error) {
	if runtime.GOOS == "windows" && hostprofile.Enabled() {
		service, ok := hostprofile.ServiceFor(name)
		if !ok || service.Image == "" || service.Directory == "" || len(service.Start) == 0 {
			return marketServiceLaunch{}, fmt.Errorf("host profile has no complete %s launch contract", name)
		}
		return marketServiceLaunch{dir: filepath.Dir(service.Image), bin: service.Image,
			args: append([]string(nil), service.Start...), source: "host-profile"}, nil
	}
	runScript := strings.TrimSpace(a.serviceRunScript)
	if runScript == "" {
		runScript = defaultServiceRunScript
	}
	homeRoot := strings.TrimSpace(a.serviceHomeRoot)
	if homeRoot == "" {
		homeRoot = defaultServiceHomeRoot
	}
	launch, err := serviceinit.DiscoverLaunch(name, preferredRoot, runScript, homeRoot)
	return marketServiceLaunch{
		dir:    launch.Dir,
		bin:    launch.Bin,
		args:   append([]string(nil), launch.Args...),
		source: launch.Source,
	}, err
}
