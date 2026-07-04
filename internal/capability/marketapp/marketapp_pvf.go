package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/capability/pvf"
	"runtime"
	"strconv"
	"strings"
	"time"
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
	status := a.itemInfoStatus()
	if err := a.prepareItemInfoRelease(); err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_prepare", Status: "failed", Message: status.Error})
		a.mu.Lock()
		a.itemInfo = status
		a.mu.Unlock()
		return status
	}
	status = a.syncItemInfoDAT()
	a.mu.Lock()
	a.itemInfo = status
	a.auctionQueue = nil
	a.auctionRejected = nil
	a.auctionQueueSource = ""
	a.mu.Unlock()
	return status
}

func (a *App) prepareItemInfoRelease() error {
	wasAutoRunning := a.AutoRunning()
	if wasAutoRunning {
		a.StopAuto()
		defer a.StartAuto()
	}
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if err := a.clearSystemMarketStockBeforeItemInfo("auction", a.cfg.AuctionDB, "127.0.0.1:30803", "./df_auction_r"); err != nil {
		return err
	}
	if err := a.clearSystemMarketStockBeforeItemInfo("cera", a.cfg.CeraDB, "127.0.0.1:30603", "./df_point_r"); err != nil {
		return err
	}
	a.mu.Lock()
	a.auctionQueue = nil
	a.auctionRejected = nil
	a.auctionQueueSource = ""
	a.mu.Unlock()
	return nil
}

func (a *App) clearSystemMarketStockBeforeItemInfo(market, dbName, addr, bin string) error {
	count, err := a.repository.CountSystemStock(dbName, a.cfg.SystemOwner.IDBase)
	if err != nil {
		return fmt.Errorf("%s count system stock: %w", market, err)
	}
	if count <= 0 {
		a.appendLog(LogEvent{Type: "iteminfo_prepare", Market: market, Status: "empty", Message: "system stock already empty"})
		return nil
	}
	if tcpReady(addr, 500*time.Millisecond) && marketServicePID(bin) > 0 {
		if err := a.collectSystemMarketStock(market, dbName, count); err != nil {
			return err
		}
	} else {
		deleted, err := a.repository.DeleteSystemStock(dbName, a.cfg.SystemOwner.IDBase)
		if err != nil {
			return fmt.Errorf("%s delete system stock: %w", market, err)
		}
		a.appendLog(LogEvent{Type: "iteminfo_prepare", Market: market, Status: "db_deleted", Message: fmt.Sprintf("rows=%d", deleted)})
	}
	return a.waitSystemStockEmpty(market, dbName, 30*time.Second)
}

func (a *App) collectSystemMarketStock(market, dbName string, expected int) error {
	rows, err := a.repository.LoadSystemCollectRows(dbName, market, a.cfg.SystemOwner.IDBase)
	if err != nil {
		return fmt.Errorf("%s load system collect rows: %w", market, err)
	}
	if len(rows) == 0 {
		return nil
	}
	result := PlanResult{GeneratedAt: time.Now()}
	a.appendCollectActions(rows, &result)
	jobID := fmt.Sprintf("iteminfo-collect-%s-%d", market, time.Now().UnixNano())
	a.appendLog(LogEvent{Type: "iteminfo_prepare", JobID: jobID, Market: market, Status: "collect_start", Message: fmt.Sprintf("rows=%d expected=%d", len(rows), expected)})
	failed, _, firstErr := a.executeActions(jobID, result.Actions, a.cfg.Collector.MaxConcurrent, true, &JobSummary{ID: jobID})
	if firstErr != nil {
		return fmt.Errorf("%s collect system stock failed=%d: %w", market, failed, firstErr)
	}
	if failed > 0 {
		return fmt.Errorf("%s collect system stock failed actions=%d", market, failed)
	}
	a.appendLog(LogEvent{Type: "iteminfo_prepare", JobID: jobID, Market: market, Status: "collect_sent", Message: fmt.Sprintf("rows=%d", len(rows))})
	return nil
}

func (a *App) waitSystemStockEmpty(market, dbName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last int
	for {
		count, err := a.repository.CountSystemStock(dbName, a.cfg.SystemOwner.IDBase)
		if err != nil {
			return fmt.Errorf("%s count system stock: %w", market, err)
		}
		last = count
		if count == 0 {
			a.appendLog(LogEvent{Type: "iteminfo_prepare", Market: market, Status: "clean", Message: "system stock empty"})
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s system stock not empty after cleanup: rows=%d", market, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (a *App) currentItemInfoIDs() (map[uint32]bool, string, error) {
	for _, target := range a.cfg.ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if err := a.auctionServiceLoadedItemInfo(target); err != nil {
			return nil, target, err
		}
		ids, err := readItemInfoIDs(target)
		if err != nil {
			continue
		}
		return ids, target, nil
	}
	return nil, "", fmt.Errorf("no readable iteminfo target")
}

func (a *App) auctionServiceLoadedItemInfo(path string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	pid := marketServicePID("./df_auction_r")
	if pid <= 0 {
		return nil
	}
	started, err := linuxProcessStartTime(pid)
	if err != nil {
		return err
	}
	if info.ModTime().After(started.Add(time.Second)) {
		return fmt.Errorf("iteminfo.dat is newer than df_auction_r start: iteminfo=%s df_auction_r=%s; wait for user restart", info.ModTime().Format(time.RFC3339), started.Format(time.RFC3339))
	}
	return nil
}

func linuxProcessStartTime(pid int) (time.Time, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, err
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return time.Time{}, fmt.Errorf("invalid proc stat for pid %d", pid)
	}
	fields := strings.Fields(string(stat[end+2:]))
	if len(fields) < 20 {
		return time.Time{}, fmt.Errorf("invalid proc stat field count for pid %d", pid)
	}
	startTicks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	boot, err := linuxBootTime()
	if err != nil {
		return time.Time{}, err
	}
	return boot.Add(time.Duration(startTicks) * time.Second / time.Duration(linuxClockTicks())), nil
}

func linuxBootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "btime" {
			sec, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("btime not found in /proc/stat")
}

func linuxClockTicks() int64 {
	return 100
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
