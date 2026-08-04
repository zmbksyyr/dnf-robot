package marketapp

import (
	"strings"

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
