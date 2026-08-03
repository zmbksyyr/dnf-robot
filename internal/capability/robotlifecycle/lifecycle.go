package robotlifecycle

import (
	"errors"
	"fmt"
	equipcap "robot/internal/capability/equipment"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robotspawn "robot/internal/capability/robotspawn"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
	"time"
)

type RobotIDAllocation struct {
	UIDs     []int
	FirstCID int
}

type CreateCatalogs struct {
	Equipment []shared.EquipmentCatalogItem
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
	RobotLocations() ([]shared.MapLocation, error)
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

type createBatchRecoveryEnv interface {
	RecoverIncompleteCreateBatches() error
	BeginCreateBatch(batchID string, uids, cids []int) error
	CompleteCreateBatch(batchID string) error
	RollbackCreateBatch(batchID string) error
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
	batchEnv, batchRecovery := env.(createBatchRecoveryEnv)
	if batchRecovery {
		if err := batchEnv.RecoverIncompleteCreateBatches(); err != nil {
			return nil, fmt.Errorf("recover interrupted robot creation: %w", err)
		}
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
	if len(allocation.UIDs) != req.Count || allocation.FirstCID <= 0 {
		return nil, fmt.Errorf("invalid robot ID allocation: uids=%d count=%d first_cid=%d", len(allocation.UIDs), req.Count, allocation.FirstCID)
	}
	batchID := ""
	if batchRecovery {
		cids := make([]int, len(allocation.UIDs))
		for index := range cids {
			cids[index] = allocation.FirstCID + index
		}
		batchID = fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), allocation.UIDs[0], allocation.FirstCID)
		if err := batchEnv.BeginCreateBatch(batchID, allocation.UIDs, cids); err != nil {
			return nil, fmt.Errorf("begin robot creation batch: %w", err)
		}
	}
	robots := make([]robotcap.Info, 0, req.Count)
	usedNames := make(map[string]struct{}, req.Count)
	levels := make([]int, req.Count)
	for i := range levels {
		levels[i] = env.RandBetween(rc.LevelMin, rc.LevelMax)
	}
	locations, err := env.RobotLocations()
	if err != nil {
		if batchRecovery {
			return nil, errors.Join(err, batchEnv.RollbackCreateBatch(batchID))
		}
		return nil, err
	}
	spawnTargets, hasSpawnTargets := distributedSpawnTargets(env, maps, levels, locations)
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
		if hasSpawnTargets && i < len(spawnTargets) && spawnTargets[i].mp.Use {
			target := spawnTargets[i]
			mp := target.mp
			info.Village = mp.Village
			info.Area = mp.Area
			info.X = target.x
			info.Y = target.y
		} else if mp, ok := env.RandomMap(maps, info.Level); ok {
			info.Village = mp.Village
			info.Area = mp.Area
			if x, y, pointOK := robotspawn.RandomPointInMap(env, mp); pointOK {
				info.X, info.Y = x, y
			}
		}
		env.ApplyConfiguredLocation(&info, rc, maps)
		if err := c.createRobot(info, rc, catalogs); err != nil {
			if batchRecovery {
				rollbackErr := batchEnv.RollbackCreateBatch(batchID)
				return nil, errors.Join(err, rollbackErr)
			}
			return robots, err
		}
		robots = append(robots, info)
	}
	if batchRecovery {
		if err := batchEnv.CompleteCreateBatch(batchID); err != nil {
			rollbackErr := batchEnv.RollbackCreateBatch(batchID)
			return nil, errors.Join(fmt.Errorf("complete robot creation batch: %w", err), rollbackErr)
		}
	}
	return robots, nil
}

type spawnTarget struct {
	mp shared.MapCatalogItem
	x  int
	y  int
}

func distributedSpawnTargets(env CreateEnv, maps []shared.MapCatalogItem, levels []int, locations []shared.MapLocation) ([]spawnTarget, bool) {
	targets, ok := robotspawn.DistributedTargets(env, maps, levels, locations)
	if !ok {
		return nil, false
	}
	out := make([]spawnTarget, len(targets))
	for index, target := range targets {
		out[index] = spawnTarget{mp: target.Map, x: target.X, y: target.Y}
	}
	return out, true
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
	if err := env.CopyTemplateDefaults(info.CID); err != nil {
		foundationlog.Robotf("[RobotCreate] optional template defaults skipped cid=%d err=%v\n", info.CID, err)
	}
	if err := env.EquipFromCatalog(info.CID, info.Level, info.Job, rc, catalogs.Equipment); err != nil {
		return err
	}
	if err := env.AvatarFromCatalog(info.CID, info.Level, info.Job, rc, catalogs.Equipment); err != nil {
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
	BatchDeleteRobotMetadata(uids []int) error
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
	fullDeleteIndexes := make([]int, 0, len(candidates))
	fullUIDs := make([]int, 0, len(candidates))
	fullCIDs := make([]int, 0, len(candidates))
	metadataDeleteIndexes := make([]int, 0, len(candidates))
	metadataUIDs := make([]int, 0, len(candidates))
	for i, candidate := range candidates {
		if candidate.Protected {
			result.Skipped++
			continue
		}
		if candidate.MetadataOnly {
			metadataDeleteIndexes = append(metadataDeleteIndexes, i)
			metadataUIDs = append(metadataUIDs, candidate.UID)
			continue
		}
		fullDeleteIndexes = append(fullDeleteIndexes, i)
		fullUIDs = append(fullUIDs, candidate.UID)
		if candidate.CID > 0 {
			fullCIDs = append(fullCIDs, candidate.CID)
		}
	}
	if len(fullUIDs) == 0 && len(metadataUIDs) == 0 {
		return result, nil
	}
	allUIDs := append(append([]int(nil), fullUIDs...), metadataUIDs...)
	finishDelete := env.PrepareDelete(allUIDs)
	if finishDelete != nil {
		defer finishDelete()
	}
	if len(metadataUIDs) > 0 {
		if err := env.BatchDeleteRobotMetadata(metadataUIDs); err != nil {
			for _, i := range metadataDeleteIndexes {
				result.Candidates[i].Protected = true
				result.Candidates[i].Reason = err.Error()
				result.Skipped++
			}
		} else {
			for _, i := range metadataDeleteIndexes {
				result.Candidates[i].Deleted = true
				result.Deleted++
			}
		}
	}
	if len(fullUIDs) > 0 {
		if err := env.BatchDeleteRobotData(fullUIDs, fullCIDs); err != nil {
			for _, i := range fullDeleteIndexes {
				result.Candidates[i].Protected = true
				result.Candidates[i].Reason = err.Error()
				result.Skipped++
			}
		} else {
			for _, i := range fullDeleteIndexes {
				result.Candidates[i].Deleted = true
				result.Deleted++
			}
		}
	}
	return result, nil
}
