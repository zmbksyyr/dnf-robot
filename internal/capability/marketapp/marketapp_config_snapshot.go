package marketapp

import "time"

// Config values published on App are immutable. Writers replace the complete
// snapshot; readers copy only the struct header while holding stateMu, so they
// do not retain the lock while using slices and pointers.
func (a *App) configSnapshot() Config {
	if a == nil {
		return Config{}
	}
	a.stateMu.RLock()
	cfg := a.cfg
	a.stateMu.RUnlock()
	return cfg
}

func (a *App) setConfig(cfg Config) {
	cfg = cloneConfig(cfg)
	a.stateMu.Lock()
	databaseChanged := a.cfg.GameDB != cfg.GameDB || a.cfg.AuctionDB != cfg.AuctionDB || a.cfg.CeraDB != cfg.CeraDB
	a.cfg = cfg
	if a.dbGeneration == 0 {
		a.dbGeneration = 1
	} else if databaseChanged {
		a.dbGeneration++
	}
	if databaseChanged {
		a.dbInit = nil
		a.dbInitErr = ""
		a.dbInitOK = false
		a.dbRetryAt = time.Time{}
	}
	a.stateMu.Unlock()
}

func cloneConfig(cfg Config) Config {
	out := cfg
	out.ItemInfoTargets = append([]string(nil), cfg.ItemInfoTargets...)
	out.Restock.Comments = cloneStringMap(cfg.Restock.Comments)
	out.Restock.BlockedItemIDs = append([]uint32(nil), cfg.Restock.BlockedItemIDs...)
	out.Restock.AllowedItemIDs = append([]uint32(nil), cfg.Restock.AllowedItemIDs...)
	out.Restock.StackSizes = append([]int(nil), cfg.Restock.StackSizes...)
	out.Cera.Comments = cloneStringMap(cfg.Cera.Comments)
	out.Cera.Items = append([]ceraRow(nil), cfg.Cera.Items...)
	out.Auto.Markets = append([]string(nil), cfg.Auto.Markets...)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
