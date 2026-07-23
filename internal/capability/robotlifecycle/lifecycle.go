package robotlifecycle

import (
	"fmt"
	"math"
	equipcap "robot/internal/capability/equipment"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type RobotIDAllocation struct {
	UIDs     []int
	FirstCID int
}

type CreateCatalogs struct {
	Equipment []shared.EquipmentCatalogItem
	Stackable []shared.EquipmentCatalogItem
}

type CreateEnv interface {
	AllocateRobotIDs(count, uidStart, uidEnd int) (RobotIDAllocation, error)
	AvatarFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error
	ApplyConfiguredLocation(info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem)
	Config() robotconfig.RuntimeConfig
	CopyTemplateDefaults(cid int) error
	CreateBaseCharacter(info robotcap.Info, rc robotconfig.RuntimeConfig) error
	EnsureAccount(uid int, innerIP string) error
	EnsureWorldHornByCID(cid int) error
	EnsureSchema() error
	EquipFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error
	LoadCreateCatalogs() CreateCatalogs
	LoadMapCatalog() []shared.MapCatalogItem
	PopulateInventory(info robotcap.Info, rc robotconfig.RuntimeConfig, items []shared.EquipmentCatalogItem) error
	PrepareRobotUIDRange(uidStart, uidEnd, uidGuard int) error
	RebuildCharacView(uid int) error
	RegisterRobot(info robotcap.Info) error
	RandomFrom(vals []int) int
	RandomMap(maps []shared.MapCatalogItem, level int) (shared.MapCatalogItem, bool)
	RandBetween(min, max int) int
	RobotGamePort() int
	RobotInnerIP() string
	RobotName(uid int, used map[string]struct{}, rc robotconfig.RuntimeConfig) string
	UpsertDummy(info robotcap.Info, innerIP string) error
}

type Creator struct {
	Env CreateEnv
}

func (c Creator) Create(req robotcap.CreateRequest) ([]robotcap.Info, error) {
	env := c.Env
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 200 {
		req.Count = 200
	}
	rc := env.Config()
	maps := env.LoadMapCatalog()
	if err := env.EnsureSchema(); err != nil {
		return nil, err
	}
	if rc.RobotUIDGuard != 0 && rc.RobotUIDGuard <= rc.RobotUIDEnd {
		return nil, fmt.Errorf("robot_uid_guard %d must be greater than robot_uid_end %d, or 0 to disable", rc.RobotUIDGuard, rc.RobotUIDEnd)
	}
	catalogs := env.LoadCreateCatalogs()
	jobs := equipcap.FilterAvatarSupportedJobs(rc.Jobs, catalogs.Equipment, rc)
	if len(rc.Jobs) > 0 && len(jobs) == 0 {
		return nil, fmt.Errorf("configured jobs %v have no PVF avatar support for at least %d slots", rc.Jobs, rc.MinAvatarSlots)
	}
	if err := env.PrepareRobotUIDRange(rc.RobotUIDStart, rc.RobotUIDEnd, rc.RobotUIDGuard); err != nil {
		return nil, err
	}
	allocation, err := env.AllocateRobotIDs(req.Count, rc.RobotUIDStart, rc.RobotUIDEnd)
	if err != nil {
		return nil, err
	}
	robots := make([]robotcap.Info, 0, req.Count)
	usedNames := make(map[string]struct{}, req.Count)
	levels := make([]int, req.Count)
	for i := range levels {
		levels[i] = env.RandBetween(rc.LevelMin, rc.LevelMax)
	}
	spawnMaps, hasSpawnMaps := distributedSpawnMaps(env, maps, levels)
	for i := 0; i < req.Count; i++ {
		info := robotcap.Info{
			UID:     allocation.UIDs[i],
			CID:     allocation.FirstCID + i,
			Name:    env.RobotName(allocation.UIDs[i], usedNames, rc),
			Level:   levels[i],
			Job:     env.RandomFrom(jobs),
			Grow:    env.RandomFrom(rc.GrowTypes),
			Port:    env.RobotGamePort(),
			Village: rc.SpawnFallbackVillage,
			Area:    rc.SpawnArea,
			X:       env.RandBetween(rc.SpawnXMin, rc.SpawnXMax),
			Y:       env.RandBetween(rc.SpawnYMin, rc.SpawnYMax),
		}
		if hasSpawnMaps && i < len(spawnMaps) && spawnMaps[i].Use {
			mp := spawnMaps[i]
			info.Village = mp.Village
			info.Area = mp.Area
			info.X = env.RandBetween(mp.XMin, mp.XMax)
			info.Y = env.RandBetween(mp.YMin, mp.YMax)
		} else if mp, ok := env.RandomMap(maps, info.Level); ok {
			info.Village = mp.Village
			info.Area = mp.Area
			info.X = env.RandBetween(mp.XMin, mp.XMax)
			info.Y = env.RandBetween(mp.YMin, mp.YMax)
		}
		env.ApplyConfiguredLocation(&info, rc, maps)
		if err := c.createRobot(info, rc, catalogs); err != nil {
			return robots, err
		}
		robots = append(robots, info)
	}
	return robots, nil
}

type spawnMapCandidate struct {
	mp     shared.MapCatalogItem
	weight int
}

func distributedSpawnMaps(env CreateEnv, maps []shared.MapCatalogItem, levels []int) ([]shared.MapCatalogItem, bool) {
	if len(levels) == 0 {
		return nil, false
	}
	candidates := make([]spawnMapCandidate, 0, len(maps))
	for _, mp := range maps {
		if mp.Use && mp.Village >= 0 && mp.Area >= 0 {
			candidates = append(candidates, spawnMapCandidate{mp: mp, weight: smoothedMapAreaWeight(mp)})
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	out := make([]shared.MapCatalogItem, 0, len(levels))
	assigned := make([]int, len(candidates))
	for _, level := range levels {
		eligible := eligibleSpawnMapIndexes(candidates, assigned, level, true)
		if len(eligible) == 0 {
			eligible = eligibleSpawnMapIndexes(candidates, assigned, level, false)
		}
		if len(eligible) == 0 {
			out = append(out, shared.MapCatalogItem{})
			continue
		}
		chosen := weightedSpawnMapIndex(env, candidates, eligible)
		assigned[chosen]++
		out = append(out, candidates[chosen].mp)
	}
	return out, true
}

func eligibleSpawnMapIndexes(candidates []spawnMapCandidate, assigned []int, level int, unassignedOnly bool) []int {
	eligible := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if c.mp.Level > level {
			continue
		}
		if unassignedOnly && assigned[i] > 0 {
			continue
		}
		eligible = append(eligible, i)
	}
	return eligible
}

func weightedSpawnMapIndex(env CreateEnv, candidates []spawnMapCandidate, indexes []int) int {
	total := 0
	for _, idx := range indexes {
		total += candidates[idx].weight
	}
	if total <= 0 {
		return indexes[0]
	}
	choice := env.RandBetween(0, total-1)
	if choice < 0 || choice >= total {
		choice = 0
	}
	for _, idx := range indexes {
		weight := candidates[idx].weight
		if choice < weight {
			return idx
		}
		choice -= weight
	}
	return indexes[len(indexes)-1]
}

func smoothedMapAreaWeight(mp shared.MapCatalogItem) int {
	width := mp.XMax - mp.XMin + 1
	height := mp.YMax - mp.YMin + 1
	if width <= 0 || height <= 0 {
		return 1
	}
	weight := int(math.Sqrt(float64(width * height)))
	if weight < 1 {
		return 1
	}
	return weight
}

func (c Creator) createRobot(info robotcap.Info, rc robotconfig.RuntimeConfig, catalogs CreateCatalogs) error {
	env := c.Env
	innerIP := env.RobotInnerIP()
	if err := env.EnsureAccount(info.UID, innerIP); err != nil {
		return err
	}
	if err := env.CreateBaseCharacter(info, rc); err != nil {
		return err
	}
	_ = env.CopyTemplateDefaults(info.CID)
	if err := env.EquipFromCatalog(info.CID, info.Level, info.Job, rc, catalogs.Equipment); err != nil {
		return err
	}
	if err := env.AvatarFromCatalog(info.CID, info.Level, info.Job, rc, catalogs.Equipment); err != nil {
		return err
	}
	if err := env.PopulateInventory(info, rc, catalogs.Stackable); err != nil {
		return err
	}
	if err := env.EnsureWorldHornByCID(info.CID); err != nil {
		return err
	}
	if err := env.RebuildCharacView(info.UID); err != nil {
		return err
	}
	if err := env.UpsertDummy(info, innerIP); err != nil {
		return err
	}
	return env.RegisterRobot(info)
}

type CleanupEnv interface {
	BatchDeleteRobotData(uids, cids []int) error
	CleanupCandidates(req robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, error)
	EnsureSchema() error
	PrepareDelete(uids []int) func()
}

type Cleaner struct {
	Env CleanupEnv
}

func (c Cleaner) Cleanup(req robotcap.CleanupRequest) (robotcap.CleanupResult, error) {
	env := c.Env
	if err := env.EnsureSchema(); err != nil {
		return robotcap.CleanupResult{}, err
	}
	candidates, err := env.CleanupCandidates(req)
	if err != nil {
		return robotcap.CleanupResult{}, err
	}
	result := robotcap.CleanupResult{DryRun: !req.Force, Requested: len(candidates), Candidates: candidates}
	if !req.Force {
		for _, candidate := range candidates {
			if candidate.Protected {
				result.Skipped++
			}
		}
		return result, nil
	}
	deleteIndexes := make([]int, 0, len(candidates))
	uids := make([]int, 0, len(candidates))
	cids := make([]int, 0, len(candidates))
	for i, candidate := range candidates {
		if candidate.Protected {
			result.Skipped++
			continue
		}
		deleteIndexes = append(deleteIndexes, i)
		uids = append(uids, candidate.UID)
		if candidate.CID > 0 {
			cids = append(cids, candidate.CID)
		}
	}
	if len(uids) == 0 {
		return result, nil
	}
	finishDelete := env.PrepareDelete(uids)
	if finishDelete != nil {
		defer finishDelete()
	}
	if err := env.BatchDeleteRobotData(uids, cids); err != nil {
		for _, i := range deleteIndexes {
			result.Candidates[i].Protected = true
			result.Candidates[i].Reason = err.Error()
			result.Skipped++
		}
		return result, nil
	}
	for _, i := range deleteIndexes {
		result.Candidates[i].Deleted = true
		result.Deleted++
	}
	return result, nil
}
