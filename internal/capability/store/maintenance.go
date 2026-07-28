package store

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robotspawn "robot/internal/capability/robotspawn"
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
	RobotLocations() ([]shared.MapLocation, error)
	ResetPrivateStore(uid int)
	RestoreDummyNormal(info robotcap.Info) error
	RevokeStorePermission(uid, cid int) error
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
	SyncCharacterVillage(cid int, village int) (int, error)
}

func (m Maintenance) RestoreAutoNormalPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) (robotcap.Info, error) {
	env := m.Env
	maps := env.LoadMapCatalog()
	normal := m.randomNormalPosition(info, rc, maps)
	if err := env.RestoreDummyNormal(normal); err != nil {
		env.Logf("[AutoStore] uid=%d restore_normal_write_failed reason=%s pos=%d/%d/%d/%d err=%v\n",
			normal.UID, reason, normal.Village, normal.Area, normal.X, normal.Y, err)
		return normal, err
	}
	if statPrev, err := env.SyncCharacterVillage(normal.CID, normal.Village); err != nil {
		env.Logf("[AutoStore] uid=%d restore_charac_village_sync_failed cid=%d village=%d err=%v\n",
			normal.UID, normal.CID, normal.Village, err)
	} else {
		env.Logf("[AutoStore] cid=%d charac_village_synced village=%d stat_prev=%d\n", normal.CID, normal.Village, statPrev)
	}
	env.Logf("[AutoStore] uid=%d restore_normal reason=%s pos=%d/%d/%d/%d\n",
		normal.UID, reason, normal.Village, normal.Area, normal.X, normal.Y)
	return normal, nil
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
	normalMaps := FilterNormalMaps(maps)
	locations, locationErr := env.RobotLocations()
	balanced := false
	if locationErr == nil {
		if target, ok := robotspawn.BalancedLocation(env, normalMaps, normal.Level, locations); ok {
			normal.Village = target.Map.Village
			normal.Area = target.Map.Area
			normal.X = target.X
			normal.Y = target.Y
			balanced = true
		}
	} else {
		env.Logf("[AutoStore] uid=%d restore_normal_locations_failed err=%v\n", normal.UID, locationErr)
	}
	if !balanced {
		if mp, ok := env.RandomMap(normalMaps, normal.Level); ok {
			normal.Village = mp.Village
			normal.Area = mp.Area
			if x, y, pointOK := robotspawn.RandomPointInMap(env, mp); pointOK {
				normal.X, normal.Y = x, y
			}
		}
	}
	env.ApplyConfiguredLocation(&normal, rc, normalMaps)
	if !IsNormalAreaEligible(normal.Village, normal.Area) {
		normal.Village = rc.SpawnFallbackVillage
		normal.Area = rc.SpawnArea
		normal.X = env.RandBetween(rc.SpawnXMin, rc.SpawnXMax)
		normal.Y = env.RandBetween(rc.SpawnYMin, rc.SpawnYMax)
		if !IsNormalAreaEligible(normal.Village, normal.Area) {
			normal.Village = 1
			normal.Area = 0
		}
	}
	return normal
}

func FilterNormalMaps(maps []shared.MapCatalogItem) []shared.MapCatalogItem {
	if len(maps) == 0 {
		return nil
	}
	out := make([]shared.MapCatalogItem, 0, len(maps))
	for _, mp := range maps {
		if mp.Use && (mp.Gate || IsNormalAreaEligible(mp.Village, mp.Area)) {
			out = append(out, mp)
		}
	}
	return out
}
