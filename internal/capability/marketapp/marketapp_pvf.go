package marketapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/capability/pvf"
	"robot/internal/foundation/layout"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (a *App) itemInfoStatus() ItemInfoSyncStatus {
	cfg := a.configSnapshot()
	return ItemInfoSyncStatus{
		SourcePath: layout.New(a.configDir).PVFItemInfo(),
		Targets:    append([]string(nil), cfg.ItemInfoTargets...),
	}
}

func (a *App) syncItemInfoDAT() ItemInfoSyncStatus {
	return a.syncItemInfoDATFrom(a.itemInfoStatus())
}

func (a *App) syncItemInfoDATFrom(status ItemInfoSyncStatus) ItemInfoSyncStatus {
	cfg := a.configSnapshot()
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
	nativeRows, err := loadItemInfoRows(status.Targets)
	if err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_native_merge", Status: marketLogStatusFailed, Message: status.Error})
		return status
	}
	incompatibleIDs, err := a.clientIncompatibleItemInfoIDs()
	if err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_native_filter", Status: marketLogStatusFailed, Message: status.Error})
		return status
	}
	filtered := 0
	for id := range incompatibleIDs {
		if _, ok := nativeRows[id]; ok {
			delete(nativeRows, id)
			filtered++
		}
	}
	if filtered > 0 {
		// Native iteminfo rows normally preserve service-only IDs, but an old
		// target must never restore equipment that PVF marked incompatible.
		a.appendLog(LogEvent{Type: "iteminfo_native_filter", Status: marketLogStatusSuccess, Message: fmt.Sprintf("rows=%d", filtered)})
	}
	merged, changed := mergeItemInfoOverlay(source, nativeRows)
	if err := validateConfiguredCeraItemInfo(merged, cfg.Cera.Items); err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
		return status
	}
	if changed {
		info, statErr := os.Stat(status.SourcePath)
		if statErr != nil {
			status.Error = fmt.Sprintf("stat source %s: %v", status.SourcePath, statErr)
			a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
			return status
		}
		if err := replaceItemInfoFile(status.SourcePath, merged, info.Mode().Perm()); err != nil {
			status.Error = fmt.Sprintf("merge native iteminfo %s: %v", status.SourcePath, err)
			a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
			return status
		}
		a.appendLog(LogEvent{Type: "iteminfo_native_merge", Status: marketLogStatusSuccess, Message: fmt.Sprintf("rows=%d", len(nativeRows))})
		source = merged
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
		equal, err := itemInfoFileEquals(target, source)
		if err == nil && equal {
			status.Skipped++
			continue
		}
		mode := os.FileMode(0644)
		if info, statErr := os.Stat(target); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := replaceItemInfoFile(target, source, mode); err != nil {
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

func (a *App) clientIncompatibleItemInfoIDs() (map[uint32]bool, error) {
	items, err := readPVFItems(layout.New(a.configDir).PVFEquipment())
	if err != nil {
		return nil, fmt.Errorf("read PVF equipment compatibility markers: %w", err)
	}
	ids := make(map[uint32]bool)
	for _, item := range items {
		if item.ID > 0 && item.ClientIncompatible {
			ids[uint32(item.ID)] = true
		}
	}
	return ids, nil
}

func (a *App) SyncItemInfoDAT() ItemInfoSyncStatus {
	defer a.startAutoIfEnabled()
	sourcePath, err := pvf.EnsurePVFItemInfoDAT(a.pvfPath, layout.New(a.configDir).PVF)
	if err != nil {
		status := a.itemInfoStatus()
		status.Error = fmt.Sprintf("export source %s: %v", a.pvfPath, err)
		a.appendLog(LogEvent{Type: "iteminfo_export", Status: marketLogStatusFailed, Message: status.Error})
		a.stateMu.Lock()
		a.itemInfo = status
		a.stateMu.Unlock()
		return status
	}
	status := a.itemInfoStatus()
	status.SourcePath = sourcePath
	if err := a.prepareItemInfoRelease(); err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_prepare", Status: marketLogStatusFailed, Message: status.Error})
		a.stateMu.Lock()
		a.itemInfo = status
		a.stateMu.Unlock()
		return status
	}
	serviceStates := a.marketServiceRunningStates()
	status = a.syncItemInfoDATFrom(status)
	if status.Error == "" {
		if err := a.restartMarketServicesAfterItemInfo(serviceStates); err != nil {
			status.Error = err.Error()
			a.appendLog(LogEvent{Type: "iteminfo_restart", Status: marketLogStatusFailed, Message: status.Error})
		}
	}
	a.stateMu.Lock()
	a.itemInfo = status
	a.resetAuctionQueuesLocked()
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
		a.resetCeraRejected()
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
	a.resetCeraRejected()
	return nil
}

func (a *App) resetAuctionQueues() {
	a.stateMu.Lock()
	a.resetAuctionQueuesLocked()
	a.stateMu.Unlock()
}

func (a *App) resetAuctionQueuesLocked() {
	a.auctionQueue = nil
	a.auctionSpecialQueue = nil
	a.auctionRejected = nil
	a.auctionRejectedMeta = nil
	a.auctionRejectedTick = 0
	a.auctionQueueSource = ""
	a.addInfoMu.Lock()
	a.specialAddInfo = 0
	a.addInfoMu.Unlock()
}

func (a *App) clearSystemMarketStockLocked(logType string) (ClearSystemStockResult, error) {
	cfg := a.configSnapshot()
	result := ClearSystemStockResult{}
	markets := []struct {
		name string
		db   string
	}{
		{name: marketNameAuction, db: cfg.AuctionDB},
		{name: marketNameCera, db: cfg.CeraDB},
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
	cfg := a.configSnapshot()
	result := ClearSystemMarketResult{Market: market, DBName: cfg.GameDB}
	count, err := a.repository.CountSystemCreatureItems(cfg.GameDB, cfg.SystemOwner.IDBase)
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
	deleted, err := a.repository.DeleteSystemCreatureItems(cfg.GameDB, cfg.SystemOwner.IDBase)
	if err != nil {
		result.Status = marketLogStatusDeleteFailed
		return result, fmt.Errorf("%s delete system instances: %w", market, err)
	}
	result.Deleted = deleted
	after, err := a.repository.CountSystemCreatureItems(cfg.GameDB, cfg.SystemOwner.IDBase)
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
	ownerBase := a.configSnapshot().SystemOwner.IDBase
	result := ClearSystemMarketResult{Market: market, DBName: dbName}
	count, err := a.repository.CountSystemStock(dbName, ownerBase)
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
	deleted, err := a.repository.DeleteSystemStock(dbName, ownerBase)
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
	after, err := a.repository.CountSystemStock(dbName, ownerBase)
	if err != nil {
		result.Status = marketLogStatusCountAfterFailed
		return result, fmt.Errorf("%s count system stock after delete: %w", market, err)
	}
	result.After = after
	result.Status = marketLogStatusDBDeleted
	return result, nil
}

func (a *App) waitSystemStockEmpty(logType, market, dbName string, timeout time.Duration) error {
	ownerBase := a.configSnapshot().SystemOwner.IDBase
	deadline := time.Now().Add(timeout)
	var last int
	for {
		count, err := a.repository.CountSystemStock(dbName, ownerBase)
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
	entries, path, err := a.currentItemInfoEntries()
	if err != nil {
		return nil, path, err
	}
	return itemInfoEntryIDSet(entries), path, nil
}

func (a *App) currentItemInfoEntries() (map[uint32]itemInfoEntry, string, error) {
	for _, target := range a.configSnapshot().ItemInfoTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if err := a.auctionServiceLoadedItemInfo(target); err != nil {
			return nil, target, err
		}
		entries, err := readItemInfoEntries(target)
		if err != nil {
			continue
		}
		return entries, target, nil
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
	auction, ok := a.marketServiceSpecByName(marketServiceNameAuction)
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

func readItemInfoEntries(path string) (map[uint32]itemInfoEntry, error) {
	entries := make(map[uint32]itemInfoEntry)
	_, err := scanItemInfoFile(path, func(id uint32, raw []byte) bool {
		line := strings.TrimSpace(string(raw))
		if itemInfoLineHasNullName(line) {
			return false
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return false
		}
		entry := itemInfoEntry{ItemType: -1}
		if len(fields) > 1 {
			if itemType, err := strconv.Atoi(fields[1]); err == nil {
				entry.ItemType = itemType
			}
		}
		entries[id] = entry
		return false
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("iteminfo target has no ids: %s", path)
	}
	return entries, nil
}

func itemInfoEntryIDSet(entries map[uint32]itemInfoEntry) map[uint32]bool {
	ids := make(map[uint32]bool, len(entries))
	for id := range entries {
		ids[id] = true
	}
	return ids
}

func itemInfoLineHasNullName(line string) bool {
	return strings.Contains(line, "== NULL")
}

func (a *App) PVFUpgradeSeparateStatus(req PVFUpgradeSeparateRequest) (pvf.PVFUpgradeSeparateStatus, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	return pvf.InspectPVFUpgradeSeparate(path)
}

func (a *App) PVFPatchUpgradeSeparate(req PVFUpgradeSeparateRequest) (pvf.PVFUpgradeSeparatePatchResult, error) {
	a.patchMu.Lock()
	defer a.patchMu.Unlock()

	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = a.pvfPath
	}
	if strings.TrimSpace(a.configDir) == "" {
		return pvf.PVFUpgradeSeparatePatchResult{Path: path}, fmt.Errorf("empty config dir")
	}
	target := req.Target
	if target <= 0 {
		target = 7
	}
	backup, err := layout.New(a.configDir).PVFUpgradeSeparateBackup(path)
	if err != nil {
		return pvf.PVFUpgradeSeparatePatchResult{Path: path}, fmt.Errorf("resolve backup for %s: %w", path, err)
	}
	return pvf.PatchPVFUpgradeSeparate(path, backup, target)
}

func (a *App) loadCatalog() (map[uint32]catalogItem, error) {
	out := map[uint32]catalogItem{}
	paths := layout.New(a.configDir)
	stackable, err := readPVFItems(paths.PVFStackable())
	if err != nil {
		return nil, err
	}
	for _, item := range stackable {
		if item.ID <= 0 {
			continue
		}
		kind := "stackable"
		if item.BadName || item.Expire {
			kind = "blocked"
		}
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, Path: item.Path, Level: item.Level, ItemType: item.ItemType, SubType: item.SubType, Slot: item.Slot, Attach: item.Attach, Rarity: item.Rarity, StackLimit: item.StackLimit, Price: int32(item.Price), Value: int32(item.Value), Trade: item.Trade, NoTrade: item.NoTrade, TradeBlock: item.TradeBlock, CanTrade: item.CanTrade, CanAuction: item.CanAuction, Auction: item.Auction, NeedMaterial: item.NeedMaterial, BasicMaterial: item.BasicMaterial, ClientIncompatible: item.ClientIncompatible}
	}
	equipment, err := readPVFItems(paths.PVFEquipment())
	if err != nil {
		return nil, err
	}
	for _, item := range equipment {
		if item.ID <= 0 {
			continue
		}
		kind := "equipment"
		if item.BadName || item.Expire {
			kind = "blocked"
		}
		out[uint32(item.ID)] = catalogItem{ItemID: uint32(item.ID), Kind: kind, Path: item.Path, Level: item.Level, ItemType: item.ItemType, SubType: item.SubType, Slot: item.Slot, Attach: item.Attach, Rarity: item.Rarity, StackLimit: item.StackLimit, Price: int32(item.Price), Value: int32(item.Value), Trade: item.Trade, NoTrade: item.NoTrade, TradeBlock: item.TradeBlock, CanTrade: item.CanTrade, CanAuction: item.CanAuction, Auction: item.Auction, NeedMaterial: item.NeedMaterial, BasicMaterial: item.BasicMaterial, ClientIncompatible: item.ClientIncompatible}
	}
	return out, nil
}

func readPVFItems(path string) ([]pvfItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var items []pvfItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}
