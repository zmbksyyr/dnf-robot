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
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: marketLogStatusFailed, Message: status.Error})
		return status
	}
	source, err := os.ReadFile(status.SourcePath)
	if err != nil {
		status.Error = fmt.Sprintf("read source %s: %v", status.SourcePath, err)
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: marketLogStatusFailed, Message: status.Error})
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
			a.appendLog(LogEvent{Type: "iteminfo_sync", Status: marketLogStatusFailed, Message: status.Error})
			return status
		}
		status.Synced++
		a.appendLog(LogEvent{Type: "iteminfo_sync", Status: marketLogStatusSynced, Message: target})
	}
	a.appendLog(LogEvent{Type: "iteminfo_sync", Status: marketLogStatusSuccess, Message: fmt.Sprintf("synced=%d skipped=%d", status.Synced, status.Skipped)})
	return status
}

func (a *App) SyncItemInfoDAT() ItemInfoSyncStatus {
	defer a.startAutoIfEnabled()
	sourcePath, err := pvf.ExportPVFItemInfoDAT(a.pvfPath, a.configDir)
	if err != nil {
		status := a.itemInfoStatus()
		status.Error = fmt.Sprintf("export source %s: %v", a.pvfPath, err)
		a.appendLog(LogEvent{Type: "iteminfo_export", Status: marketLogStatusFailed, Message: status.Error})
		a.stateMu.Lock()
		a.itemInfo = status
		a.stateMu.Unlock()
		return status
	}
	a.cfg.ItemInfoSourcePath = sourcePath
	status := a.itemInfoStatus()
	if err := a.prepareItemInfoRelease(); err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_prepare", Status: marketLogStatusFailed, Message: status.Error})
		a.stateMu.Lock()
		a.itemInfo = status
		a.stateMu.Unlock()
		return status
	}
	status = a.syncItemInfoDAT()
	if status.Error == "" {
		if err := a.restartMarketServicesAfterItemInfo(); err != nil {
			status.Error = err.Error()
			a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusFailed, Message: status.Error})
		}
	}
	a.stateMu.Lock()
	a.itemInfo = status
	a.auctionQueue = nil
	a.auctionSpecialQueue = nil
	a.auctionRejected = nil
	a.auctionRejectedTick = 0
	a.auctionQueueSource = ""
	a.stateMu.Unlock()
	return status
}

func (a *App) ClearSystemMarketStock() (ClearSystemStockResult, error) {
	if a.AutoRunning() {
		a.StopAutoAsync()
	}
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	result, err := a.clearSystemMarketStockLocked("market_clear")
	if err == nil {
		a.resetAuctionQueues()
	}
	return result, err
}

func (a *App) prepareItemInfoRelease() error {
	if a.AutoRunning() {
		a.StopAutoAsync()
	}
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if _, err := a.clearSystemMarketStockLocked("iteminfo_prepare"); err != nil {
		return err
	}
	a.resetAuctionQueues()
	return nil
}

func (a *App) resetAuctionQueues() {
	a.stateMu.Lock()
	a.auctionQueue = nil
	a.auctionSpecialQueue = nil
	a.auctionRejected = nil
	a.auctionRejectedTick = 0
	a.auctionQueueSource = ""
	a.specialAddInfo = 0
	a.stateMu.Unlock()
}

func (a *App) clearSystemMarketStockLocked(logType string) (ClearSystemStockResult, error) {
	result := ClearSystemStockResult{}
	markets := []struct {
		name string
		db   string
	}{
		{name: marketNameAuction, db: a.cfg.AuctionDB},
		{name: marketNameCera, db: a.cfg.CeraDB},
	}
	for _, market := range markets {
		item, err := a.deleteSystemMarketStock(logType, market.name, market.db)
		result.Markets = append(result.Markets, item)
		result.Deleted += item.Deleted
		if err != nil {
			return result, err
		}
	}
	item, err := a.deleteSystemCreatureItems(logType)
	result.Markets = append(result.Markets, item)
	result.Deleted += item.Deleted
	if err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) deleteSystemCreatureItems(logType string) (ClearSystemMarketResult, error) {
	const market = "creature"
	result := ClearSystemMarketResult{Market: market, DBName: a.cfg.GameDB}
	count, err := a.repository.CountSystemCreatureItems(a.cfg.GameDB, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusCountFailed
		return result, fmt.Errorf("%s count system instances: %w", market, err)
	}
	result.Before = count
	if count <= 0 {
		result.Status = marketLogStatusEmpty
		a.appendLog(LogEvent{Type: logType, Market: market, Status: result.Status, Message: "system creature instances already empty"})
		return result, nil
	}
	deleted, err := a.repository.DeleteSystemCreatureItems(a.cfg.GameDB, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusDeleteFailed
		return result, fmt.Errorf("%s delete system instances: %w", market, err)
	}
	result.Deleted = deleted
	after, err := a.repository.CountSystemCreatureItems(a.cfg.GameDB, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusCountAfterFailed
		return result, fmt.Errorf("%s count system instances after delete: %w", market, err)
	}
	result.After = after
	result.Status = marketLogStatusDBDeleted
	a.appendLog(LogEvent{Type: logType, Market: market, Status: result.Status, Message: fmt.Sprintf("rows=%d", deleted)})
	return result, nil
}

func (a *App) deleteSystemMarketStock(logType, market, dbName string) (ClearSystemMarketResult, error) {
	result := ClearSystemMarketResult{Market: market, DBName: dbName}
	count, err := a.repository.CountSystemStock(dbName, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusCountFailed
		return result, fmt.Errorf("%s count system stock: %w", market, err)
	}
	result.Before = count
	if count <= 0 {
		result.Status = marketLogStatusEmpty
		a.appendLog(LogEvent{Type: logType, Market: market, Status: result.Status, Message: "system stock already empty"})
		return result, nil
	}
	deleted, err := a.repository.DeleteSystemStock(dbName, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusDeleteFailed
		return result, fmt.Errorf("%s delete system stock: %w", market, err)
	}
	result.Deleted = deleted
	a.appendLog(LogEvent{Type: logType, Market: market, Status: marketLogStatusDBDeleted, Message: fmt.Sprintf("rows=%d", deleted)})
	if err := a.waitSystemStockEmpty(logType, market, dbName, 30*time.Second); err != nil {
		result.Status = marketLogStatusWaitFailed
		return result, err
	}
	after, err := a.repository.CountSystemStock(dbName, a.cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusCountAfterFailed
		return result, fmt.Errorf("%s count system stock after delete: %w", market, err)
	}
	result.After = after
	result.Status = marketLogStatusDBDeleted
	return result, nil
}

func (a *App) waitSystemStockEmpty(logType, market, dbName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last int
	for {
		count, err := a.repository.CountSystemStock(dbName, a.cfg.SystemOwner.IDBase)
		if err != nil {
			return fmt.Errorf("%s count system stock: %w", market, err)
		}
		last = count
		if count == 0 {
			a.appendLog(LogEvent{Type: logType, Market: market, Status: marketLogStatusClean, Message: "system stock empty"})
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
	auction, ok := marketServiceSpecByName(marketServiceNameAuction)
	if !ok {
		return fmt.Errorf("auction service spec not found")
	}
	pid := marketServicePID(auction.bin)
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
