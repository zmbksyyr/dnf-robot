package robotspawn

import (
	"strings"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/mathx"
	"robot/internal/shared"
)

type Env interface {
	FollowAccountVillage(account string) (int, bool, error)
	RandBetween(min, max int) int
	RandIntn(n int) int
}

func ApplyConfiguredLocation(env Env, info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) {
	if env == nil || info == nil {
		return
	}
	if rc.SpawnFixed {
		village := rc.SpawnVillage
		if village < 1 {
			village = 1
		}
		if village > 3 {
			village = 3
		}
		ApplyVillageLocation(env, info, village, rc, maps)
		return
	}
	account := strings.TrimSpace(rc.FollowAccount)
	if account == "" {
		return
	}
	village, ok, err := env.FollowAccountVillage(account)
	if err != nil || !ok {
		return
	}
	ApplyVillageLocation(env, info, village, rc, maps)
}

func ApplyVillageLocation(env Env, info *robotcap.Info, village int, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) {
	info.Village = village
	var candidates []shared.MapCatalogItem
	for _, mp := range maps {
		if mp.Use && mp.Village == info.Village && mp.Level <= info.Level {
			candidates = append(candidates, mp)
		}
	}
	if len(candidates) == 0 {
		return
	}
	if rc.SpawnArea >= 0 {
		var areaMatches []shared.MapCatalogItem
		for _, mp := range candidates {
			if mp.Area == rc.SpawnArea {
				areaMatches = append(areaMatches, mp)
			}
		}
		if len(areaMatches) > 0 {
			candidates = areaMatches
		}
	}
	mp := candidates[safeRandIntn(env, len(candidates))]
	info.Area = mp.Area
	xMin, xMax := mp.XMin, mp.XMax
	yMin, yMax := mp.YMin, mp.YMax
	if rxMin, rxMax, ok := mathx.IntersectRange(mp.XMin, mp.XMax, rc.SpawnXMin, rc.SpawnXMax); ok {
		xMin, xMax = rxMin, rxMax
	}
	if ryMin, ryMax, ok := mathx.IntersectRange(mp.YMin, mp.YMax, rc.SpawnYMin, rc.SpawnYMax); ok {
		yMin, yMax = ryMin, ryMax
	}
	if rc.FollowRadiusX > 0 {
		center := (xMin + xMax) / 2
		xMin = mathx.MaxInt(xMin, center-rc.FollowRadiusX)
		xMax = mathx.MinInt(xMax, center+rc.FollowRadiusX)
	}
	if rc.FollowRadiusY > 0 {
		center := (yMin + yMax) / 2
		yMin = mathx.MaxInt(yMin, center-rc.FollowRadiusY)
		yMax = mathx.MinInt(yMax, center+rc.FollowRadiusY)
	}
	info.X = env.RandBetween(xMin, xMax)
	info.Y = env.RandBetween(yMin, yMax)
}

func RandomMap(env Env, maps []shared.MapCatalogItem, level int) (shared.MapCatalogItem, bool) {
	var candidates []shared.MapCatalogItem
	for _, mp := range maps {
		if mp.Use && mp.Village >= 0 && mp.Area >= 0 && mp.Level <= level {
			candidates = append(candidates, mp)
		}
	}
	if len(candidates) == 0 {
		return shared.MapCatalogItem{}, false
	}
	return candidates[safeRandIntn(env, len(candidates))], true
}

func safeRandIntn(env Env, n int) int {
	if env == nil || n <= 0 {
		return 0
	}
	v := env.RandIntn(n)
	if v < 0 || v >= n {
		return 0
	}
	return v
}
