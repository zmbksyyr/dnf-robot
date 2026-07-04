package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/capability/pvf"
	"strconv"
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
	preview := a.previewItemInfoDAT(sourcePath)
	status := a.syncItemInfoDAT()
	a.mu.Lock()
	a.itemInfo = status
	a.itemInfoPreview = preview
	a.auctionQueue = nil
	a.auctionQueueSource = ""
	a.mu.Unlock()
	return status
}

func (a *App) previewItemInfoDAT(sourcePath string) ItemInfoPreviewStatus {
	preview := ItemInfoPreviewStatus{SourcePath: sourcePath}
	source, err := inspectItemInfoDAT(sourcePath)
	if err != nil {
		preview.Error = err.Error()
		return preview
	}
	preview.SourceIDs = len(source.IDs)
	preview.DuplicateSource = source.Duplicates
	preview.InvalidSource = source.Invalid
	preview.Level70Rows = source.Level70Rows
	preview.AllJobRows = source.AllJobRows

	for _, target := range a.cfg.ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		targetInfo, err := inspectItemInfoDAT(target)
		if err != nil {
			continue
		}
		preview.TargetPath = target
		preview.TargetIDs = len(targetInfo.IDs)
		preview.DuplicateTarget = targetInfo.Duplicates
		preview.InvalidTarget = targetInfo.Invalid
		for id := range source.IDs {
			if targetInfo.IDs[id] {
				preview.OverwrittenIDs++
			} else {
				preview.AddedIDs++
			}
		}
		for id := range targetInfo.IDs {
			if !source.IDs[id] {
				preview.PreservedIDs++
			}
		}
		return preview
	}
	preview.Error = "no readable iteminfo target for preview"
	return preview
}

type itemInfoDATInspection struct {
	IDs         map[uint32]bool
	Duplicates  int
	Invalid     int
	Level70Rows int
	AllJobRows  int
}

func inspectItemInfoDAT(path string) (itemInfoDATInspection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return itemInfoDATInspection{}, err
	}
	out := itemInfoDATInspection{IDs: map[uint32]bool{}}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil || id == 0 {
			out.Invalid++
			continue
		}
		if out.IDs[uint32(id)] {
			out.Duplicates++
		}
		out.IDs[uint32(id)] = true
		if len(fields) >= 17 {
			if fields[13] == "70" {
				out.Level70Rows++
			}
			allJobs := true
			for i := 2; i <= 12; i++ {
				if fields[i] != "1" {
					allJobs = false
					break
				}
			}
			if allJobs {
				out.AllJobRows++
			}
		}
	}
	return out, nil
}

func (a *App) currentItemInfoIDs() (map[uint32]bool, string, error) {
	for _, target := range a.cfg.ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		ids, err := readItemInfoIDs(target)
		if err != nil {
			continue
		}
		return ids, target, nil
	}
	return nil, "", fmt.Errorf("no readable iteminfo target")
}

func readItemInfoIDs(path string) (map[uint32]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ids := make(map[uint32]bool)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil || id == 0 {
			continue
		}
		ids[uint32(id)] = true
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("iteminfo target has no ids: %s", path)
	}
	return ids, nil
}

func (a *App) resolveConfigPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.configDir, path)
}
