package store

import "path/filepath"

func newTestPointCoordinator(stateDir string, logf func(string, ...interface{})) *PointCoordinator {
	return NewPointCoordinator(stateDir, filepath.Join(stateDir, "map_catalog.json"), logf)
}
