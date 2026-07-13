package store

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type Maintenance struct {
	Env MaintenanceEnv
}

type MaintenanceEnv interface {
	ApplyConfiguredLocation(info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem)
	LoadMapCatalog() []shared.MapCatalogItem
	Logf(format string, args ...interface{})
	RandBetween(min, max int) int
	RandomMap(maps []shared.MapCatalogItem, level int) (shared.MapCatalogItem, bool)
	ResetPrivateStore(uid int)
	RestoreDummyNormal(info robotcap.Info) error
	RevokeStorePermission(uid, cid int) error
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
	SyncCharacterVillage(cid int, village int) (int, error)
}

func (m Maintenance) RestoreAutoNormalPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) robotcap.Info {
	env := m.Env
	maps := env.LoadMapCatalog()
	normal := m.randomNormalPosition(info, rc, maps)
	_ = env.RestoreDummyNormal(normal)
	if statPrev, err := env.SyncCharacterVillage(normal.CID, normal.Village); err != nil {
		env.Logf("[AutoStore] uid=%d restore_charac_village_sync_failed cid=%d village=%d err=%v\n",
			normal.UID, normal.CID, normal.Village, err)
	} else {
		env.Logf("[AutoStore] cid=%d charac_village_synced village=%d stat_prev=%d\n", normal.CID, normal.Village, statPrev)
	}
	env.Logf("[AutoStore] uid=%d restore_normal reason=%s pos=%d/%d/%d/%d\n",
		normal.UID, reason, normal.Village, normal.Area, normal.X, normal.Y)
	return normal
}

func (m Maintenance) FinishStoreState(uid, cid int, reason string) {
	if uid <= 0 {
		return
	}
	env := m.Env
	if cid <= 0 {
		if robots, err := env.SelectRobots(robotcap.CommandRequest{UIDs: []int{uid}}); err == nil && len(robots) > 0 {
			cid = robots[0].CID
		}
	}
	if err := env.RevokeStorePermission(uid, cid); err != nil {
		env.Logf("[StoreCleanup] uid=%d cid=%d reason=%s err=%v\n", uid, cid, reason, err)
	}
	env.ResetPrivateStore(uid)
	env.Logf("[StoreCleanup] uid=%d cid=%d reason=%s\n", uid, cid, reason)
}

func (m Maintenance) randomNormalPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) robotcap.Info {
	env := m.Env
	normal := info
	normal.Village = rc.SpawnFallbackVillage
	normal.Area = rc.SpawnArea
	normal.X = env.RandBetween(rc.SpawnXMin, rc.SpawnXMax)
	normal.Y = env.RandBetween(rc.SpawnYMin, rc.SpawnYMax)
	safeMaps := filterEligibleMaps(maps)
	if mp, ok := env.RandomMap(safeMaps, normal.Level); ok {
		normal.Village = mp.Village
		normal.Area = mp.Area
		normal.X = env.RandBetween(mp.XMin, mp.XMax)
		normal.Y = env.RandBetween(mp.YMin, mp.YMax)
	}
	env.ApplyConfiguredLocation(&normal, rc, safeMaps)
	if !IsAreaEligible(normal.Village, normal.Area) {
		normal.Village = rc.SpawnFallbackVillage
		normal.Area = rc.SpawnArea
		normal.X = env.RandBetween(rc.SpawnXMin, rc.SpawnXMax)
		normal.Y = env.RandBetween(rc.SpawnYMin, rc.SpawnYMax)
		if !IsAreaEligible(normal.Village, normal.Area) {
			normal.Village = 1
			normal.Area = 0
		}
	}
	return normal
}

func filterEligibleMaps(maps []shared.MapCatalogItem) []shared.MapCatalogItem {
	if len(maps) == 0 {
		return nil
	}
	out := maps[:0]
	for _, mp := range maps {
		if mp.Use && IsAreaEligible(mp.Village, mp.Area) {
			out = append(out, mp)
		}
	}
	return out
}
