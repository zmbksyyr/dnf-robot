package marketapp

import (
	"strings"

	"robot/internal/service"
)

func (a *App) PVFUpgradeSeparateStatus(req PVFUpgradeSeparateRequest) (service.PVFUpgradeSeparateStatus, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	return service.InspectPVFUpgradeSeparate(path)
}

func (a *App) PVFPatchUpgradeSeparate(req PVFUpgradeSeparateRequest) (service.PVFUpgradeSeparatePatchResult, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	target := req.Target
	if target <= 0 {
		target = 7
	}
	return service.PatchPVFUpgradeSeparate(path, target)
}
