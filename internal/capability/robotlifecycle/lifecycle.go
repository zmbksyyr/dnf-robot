package robotlifecycle

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type RobotIDAllocation struct {
	UIDs     []int
	FirstCID int
}

type CreateEnv interface {
	AllocateRobotIDs(count, uidStart int) (RobotIDAllocation, error)
	AvatarFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig) error
	ApplyConfiguredLocation(info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem)
	Config() robotconfig.RuntimeConfig
	CopyTemplateDefaults(cid int) error
	CreateBaseCharacter(info robotcap.Info, rc robotconfig.RuntimeConfig) error
	EnsureAccount(uid int, innerIP string) error
	EnsureStorePermission(uid, cid int) error
	EnsureWorldHornByCID(cid int) error
	EnsureSchema() error
	EquipFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig) error
	LoadMapCatalog() []shared.MapCatalogItem
	PopulateInventory(info robotcap.Info, rc robotconfig.RuntimeConfig) error
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
	allocation, err := env.AllocateRobotIDs(req.Count, rc.RobotUIDStart)
	if err != nil {
		return nil, err
	}
	robots := make([]robotcap.Info, 0, req.Count)
	usedNames := make(map[string]struct{}, req.Count)
	for i := 0; i < req.Count; i++ {
		info := robotcap.Info{
			UID:     allocation.UIDs[i],
			CID:     allocation.FirstCID + i,
			Name:    env.RobotName(allocation.UIDs[i], usedNames, rc),
			Level:   env.RandBetween(rc.LevelMin, rc.LevelMax),
			Job:     env.RandomFrom(rc.Jobs),
			Grow:    env.RandomFrom(rc.GrowTypes),
			Port:    env.RobotGamePort(),
			Village: rc.SpawnFallbackVillage,
			Area:    rc.SpawnArea,
			X:       env.RandBetween(rc.SpawnXMin, rc.SpawnXMax),
			Y:       env.RandBetween(rc.SpawnYMin, rc.SpawnYMax),
		}
		if mp, ok := env.RandomMap(maps, info.Level); ok {
			info.Village = mp.Village
			info.Area = mp.Area
			info.X = env.RandBetween(mp.XMin, mp.XMax)
			info.Y = env.RandBetween(mp.YMin, mp.YMax)
		}
		env.ApplyConfiguredLocation(&info, rc, maps)
		if err := c.createRobot(info, rc); err != nil {
			return robots, err
		}
		robots = append(robots, info)
	}
	return robots, nil
}

func (c Creator) createRobot(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	env := c.Env
	innerIP := env.RobotInnerIP()
	if err := env.EnsureAccount(info.UID, innerIP); err != nil {
		return err
	}
	if err := env.CreateBaseCharacter(info, rc); err != nil {
		return err
	}
	_ = env.CopyTemplateDefaults(info.CID)
	if err := env.EquipFromCatalog(info.CID, info.Level, info.Job, rc); err != nil {
		return err
	}
	if err := env.AvatarFromCatalog(info.CID, info.Level, info.Job, rc); err != nil {
		return err
	}
	if err := env.PopulateInventory(info, rc); err != nil {
		return err
	}
	if err := env.EnsureWorldHornByCID(info.CID); err != nil {
		return err
	}
	if err := env.EnsureStorePermission(info.UID, info.CID); err != nil {
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
