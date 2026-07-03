package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/capability/pvf"
	"strings"
)

// ---- iteminfo.go ----
func (a *App) itemInfoStatus() ItemInfoSyncStatus {
	return ItemInfoSyncStatus{
		SourcePath: a.resolveConfigPath(a.cfg.ItemInfoSourcePath),
		Targets:    append([]string(nil), a.cfg.ItemInfoTargets...),
	}
}

func (a *App) syncItemInfoDAT() ItemInfoSyncStatus {
	status := a.itemInfoStatus()
	if status.SourcePath == "" {
		status.Error = "iteminfo source path is empty"
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: "failed", Message: status.Error})
		return status
	}
	source, err := os.ReadFile(status.SourcePath)
	if err != nil {
		status.Error = fmt.Sprintf("read source %s: %v", status.SourcePath, err)
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: "failed", Message: status.Error})
		return status
	}
	for _, target := range status.Targets {
		if target == "" {
			status.Skipped++
			continue
		}
		if _, err := os.Stat(filepath.Dir(target)); err != nil {
			status.Skipped++
			continue
		}
		current, err := os.ReadFile(target)
		if err == nil && bytes.Equal(current, source) {
			status.Skipped++
			continue
		}
		if err := os.WriteFile(target, source, 0644); err != nil {
			status.Error = fmt.Sprintf("write target %s: %v", target, err)
			a.appendLog(LogEvent{Type: "iteminfo_sync", Status: "failed", Message: status.Error})
			return status
		}
		status.Synced++
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: "synced", Message: target})
	}
	a.appendLog(LogEvent{Type: "iteminfo_sync", Status: "success", Message: fmt.Sprintf("synced=%d skipped=%d", status.Synced, status.Skipped)})
	return status
}

func (a *App) SyncItemInfoDAT() ItemInfoSyncStatus {
	sourcePath, err := pvf.ExportPVFItemInfoDAT(a.pvfPath, a.configDir)
	if err != nil {
		status := a.itemInfoStatus()
		status.Error = fmt.Sprintf("export source %s: %v", a.pvfPath, err)
		a.appendLog(LogEvent{Type: "iteminfo_export", Status: "failed", Message: status.Error})
		a.mu.Lock()
		a.itemInfo = status
		a.mu.Unlock()
		return status
	}
	a.cfg.ItemInfoSourcePath = sourcePath
	status := a.syncItemInfoDAT()
	a.mu.Lock()
	a.itemInfo = status
	a.mu.Unlock()
	return status
}

func (a *App) resolveConfigPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.configDir, path)
}

// ---- pvf.go ----
func (a *App) PVFUpgradeSeparateStatus(req PVFUpgradeSeparateRequest) (pvf.PVFUpgradeSeparateStatus, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	return pvf.InspectPVFUpgradeSeparate(path)
}

func (a *App) PVFPatchUpgradeSeparate(req PVFUpgradeSeparateRequest) (pvf.PVFUpgradeSeparatePatchResult, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	target := req.Target
	if target <= 0 {
		target = 7
	}
	return pvf.PatchPVFUpgradeSeparate(path, target)
}
