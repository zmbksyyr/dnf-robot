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
	locations, err := env.RobotLocations()
	if err != nil {
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
			info.X = env.RandBetween(target.rectangle.XMin, target.rectangle.XMax)
			info.Y = env.RandBetween(target.rectangle.YMin, target.rectangle.YMax)
		} else if mp, ok := env.RandomMap(maps, info.Level); ok {
			info.Village = mp.Village
			info.Area = mp.Area
			rectangle := randomMapRectangle(env, mp)
			info.X = env.RandBetween(rectangle.XMin, rectangle.XMax)
			info.Y = env.RandBetween(rectangle.YMin, rectangle.YMax)
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
	mp         shared.MapCatalogItem
	rectangles []shared.MapRectangle
	weight     int
}

type spawnTarget struct {
	mp        shared.MapCatalogItem
	rectangle shared.MapRectangle
}

type mapRectangleKey struct {
	area  shared.MapAreaKey
	index int
}

func distributedSpawnTargets(env CreateEnv, maps []shared.MapCatalogItem, levels []int, locations []shared.MapLocation) ([]spawnTarget, bool) {
	if len(levels) == 0 {
		return nil, false
	}
	candidates := make([]spawnMapCandidate, 0, len(maps))
	for _, mp := range maps {
		if mp.Use && mp.Village >= 0 && mp.Area >= 0 {
			rectangles := mapRectangles(mp)
			if len(rectangles) > 0 {
				candidates = append(candidates, spawnMapCandidate{mp: mp, rectangles: rectangles, weight: smoothedRectanglesWeight(rectangles)})
			}
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	areaCounts := make(map[shared.MapAreaKey]int, len(candidates))
	rectangleCounts := make(map[mapRectangleKey]int)
	for _, location := range locations {
		area := shared.MapAreaKey{Village: location.Village, Area: location.Area}
		areaCounts[area]++
		for _, candidate := range candidates {
			if mapAreaKey(candidate.mp) != area {
				continue
			}
			for rectangleIndex, rectangle := range candidate.rectangles {
				if rectangleContains(rectangle, location.X, location.Y) {
					rectangleCounts[mapRectangleKey{area: area, index: rectangleIndex}]++
					break
				}
			}
			break
		}
	}
	out := make([]spawnTarget, 0, len(levels))
	for _, level := range levels {
		eligible := eligibleSpawnMapIndexes(candidates, areaCounts, level, true)
		if len(eligible) == 0 {
			eligible = eligibleSpawnMapIndexes(candidates, areaCounts, level, false)
		}
		if len(eligible) == 0 {
			out = append(out, spawnTarget{})
			continue
		}
		chosen := randomSpawnMapIndex(env, eligible)
		if areaCounts[mapAreaKey(candidates[chosen].mp)] > 0 {
			chosen = leastLoadedSpawnMapIndex(env, candidates, eligible, areaCounts)
		}
		candidate := candidates[chosen]
		area := mapAreaKey(candidate.mp)
		rectangleIndex := selectSpawnRectangleIndex(env, candidate.rectangles, area, rectangleCounts)
		areaCounts[area]++
		rectangleCounts[mapRectangleKey{area: area, index: rectangleIndex}]++
		out = append(out, spawnTarget{mp: candidate.mp, rectangle: candidate.rectangles[rectangleIndex]})
	}
	return out, true
}

func eligibleSpawnMapIndexes(candidates []spawnMapCandidate, counts map[shared.MapAreaKey]int, level int, emptyOnly bool) []int {
	eligible := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if c.mp.Level > level {
			continue
		}
		if emptyOnly && counts[mapAreaKey(c.mp)] > 0 {
			continue
		}
		eligible = append(eligible, i)
	}
	return eligible
}

func randomSpawnMapIndex(env CreateEnv, indexes []int) int {
	choice := env.RandBetween(0, len(indexes)-1)
	if choice < 0 || choice >= len(indexes) {
		choice = 0
	}
	return indexes[choice]
}

func leastLoadedSpawnMapIndex(env CreateEnv, candidates []spawnMapCandidate, indexes []int, counts map[shared.MapAreaKey]int) int {
	best := []int{indexes[0]}
	for _, idx := range indexes[1:] {
		bestIdx := best[0]
		left := counts[mapAreaKey(candidates[idx].mp)] * candidates[bestIdx].weight
		right := counts[mapAreaKey(candidates[bestIdx].mp)] * candidates[idx].weight
		switch {
		case left < right:
			best = []int{idx}
		case left == right:
			best = append(best, idx)
		}
	}
	return randomSpawnMapIndex(env, best)
}

func mapAreaKey(mp shared.MapCatalogItem) shared.MapAreaKey {
	return shared.MapAreaKey{Village: mp.Village, Area: mp.Area}
}

func mapRectangles(mp shared.MapCatalogItem) []shared.MapRectangle {
	if len(mp.Rectangles) > 0 {
		out := make([]shared.MapRectangle, 0, len(mp.Rectangles))
		for _, rectangle := range mp.Rectangles {
			if rectangle.XMax >= rectangle.XMin && rectangle.YMax >= rectangle.YMin {
				out = append(out, rectangle)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if mp.XMax < mp.XMin || mp.YMax < mp.YMin {
		return nil
	}
	return []shared.MapRectangle{{XMin: mp.XMin, XMax: mp.XMax, YMin: mp.YMin, YMax: mp.YMax}}
}

func randomMapRectangle(env CreateEnv, mp shared.MapCatalogItem) shared.MapRectangle {
	rectangles := mapRectangles(mp)
	if len(rectangles) == 0 {
		return shared.MapRectangle{XMin: mp.XMin, XMax: mp.XMax, YMin: mp.YMin, YMax: mp.YMax}
	}
	return rectangles[randomSpawnMapIndex(env, rectangleIndexes(len(rectangles)))]
}

func selectSpawnRectangleIndex(env CreateEnv, rectangles []shared.MapRectangle, area shared.MapAreaKey, counts map[mapRectangleKey]int) int {
	empty := make([]int, 0, len(rectangles))
	all := rectangleIndexes(len(rectangles))
	for _, index := range all {
		if counts[mapRectangleKey{area: area, index: index}] == 0 {
			empty = append(empty, index)
		}
	}
	if len(empty) > 0 {
		return randomSpawnMapIndex(env, empty)
	}
	best := []int{all[0]}
	for _, index := range all[1:] {
		bestIndex := best[0]
		left := counts[mapRectangleKey{area: area, index: index}] * smoothedRectangleWeight(rectangles[bestIndex])
		right := counts[mapRectangleKey{area: area, index: bestIndex}] * smoothedRectangleWeight(rectangles[index])
		switch {
		case left < right:
			best = []int{index}
		case left == right:
			best = append(best, index)
		}
	}
	return randomSpawnMapIndex(env, best)
}

func rectangleIndexes(count int) []int {
	indexes := make([]int, count)
	for i := range indexes {
		indexes[i] = i
	}
	return indexes
}

func rectangleContains(rectangle shared.MapRectangle, x, y int) bool {
	return x >= rectangle.XMin && x <= rectangle.XMax && y >= rectangle.YMin && y <= rectangle.YMax
}

func smoothedRectanglesWeight(rectangles []shared.MapRectangle) int {
	area := 0
	for _, rectangle := range rectangles {
		area += rectangleArea(rectangle)
	}
	if area <= 0 {
		return 1
	}
	weight := int(math.Sqrt(float64(area)))
	if weight < 1 {
		return 1
	}
	return weight
}

func smoothedRectangleWeight(rectangle shared.MapRectangle) int {
	area := rectangleArea(rectangle)
	if area <= 0 {
		return 1
	}
	weight := int(math.Sqrt(float64(area)))
	if weight < 1 {
		return 1
	}
	return weight
}

func rectangleArea(rectangle shared.MapRectangle) int {
	width := rectangle.XMax - rectangle.XMin + 1
	height := rectangle.YMax - rectangle.YMin + 1
	if width <= 0 || height <= 0 {
		return 0
	}
	return width * height
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
