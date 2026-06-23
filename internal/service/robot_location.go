package service

import (
	"database/sql"
	"strings"
)

func (m *RobotManager) applyConfiguredLocation(info *RobotInfo, rc robotRuntimeConfig, maps []mapCatalogItem) {
	if info == nil {
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
		m.applyVillageLocation(info, village, rc, maps)
		return
	}
	account := strings.TrimSpace(rc.FollowAccount)
	if account == "" {
		return
	}
	var village sql.NullInt64
	err := m.db.QueryRow(`
SELECT COALESCE(NULLIF(s.village,0), c.village)
FROM d_taiwan.accounts a
JOIN taiwan_cain.charac_info c ON c.m_id=a.UID AND c.delete_flag=0
LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=c.charac_no
WHERE a.accountname=?
ORDER BY c.charac_no DESC
LIMIT 1`, account).Scan(&village)
	if err != nil || !village.Valid {
		return
	}
	m.applyVillageLocation(info, int(village.Int64), rc, maps)
}

func (m *RobotManager) applyVillageLocation(info *RobotInfo, village int, rc robotRuntimeConfig, maps []mapCatalogItem) {
	info.Village = village
	var candidates []mapCatalogItem
	for _, mp := range maps {
		if mp.Use && mp.Village == info.Village && mp.Level <= info.Level {
			candidates = append(candidates, mp)
		}
	}
	if len(candidates) == 0 {
		return
	}
	// Prefer maps in the configured birth_area (typically area 0 = town center where stores work)
	if rc.SpawnArea >= 0 {
		var areaMatches []mapCatalogItem
		for _, mp := range candidates {
			if mp.Area == rc.SpawnArea {
				areaMatches = append(areaMatches, mp)
			}
		}
		if len(areaMatches) > 0 {
			candidates = areaMatches
		}
	}
	mp := candidates[m.randIntn(len(candidates))]
	info.Area = mp.Area
	xMin, xMax := mp.XMin, mp.XMax
	yMin, yMax := mp.YMin, mp.YMax
	if rxMin, rxMax, ok := intersectRange(mp.XMin, mp.XMax, rc.SpawnXMin, rc.SpawnXMax); ok {
		xMin, xMax = rxMin, rxMax
	}
	if ryMin, ryMax, ok := intersectRange(mp.YMin, mp.YMax, rc.SpawnYMin, rc.SpawnYMax); ok {
		yMin, yMax = ryMin, ryMax
	}
	if rc.FollowRadiusX > 0 {
		center := (xMin + xMax) / 2
		xMin = maxInt(xMin, center-rc.FollowRadiusX)
		xMax = minInt(xMax, center+rc.FollowRadiusX)
	}
	if rc.FollowRadiusY > 0 {
		center := (yMin + yMax) / 2
		yMin = maxInt(yMin, center-rc.FollowRadiusY)
		yMax = minInt(yMax, center+rc.FollowRadiusY)
	}
	info.X = m.randBetween(xMin, xMax)
	info.Y = m.randBetween(yMin, yMax)
}

func intersectRange(aMin, aMax, bMin, bMax int) (int, int, bool) {
	if aMax < aMin {
		aMin, aMax = aMax, aMin
	}
	if bMax < bMin {
		bMin, bMax = bMax, bMin
	}
	minV := maxInt(aMin, bMin)
	maxV := minInt(aMax, bMax)
	if maxV < minV {
		return 0, 0, false
	}
	return minV, maxV, true
}

func (m *RobotManager) randomMap(maps []mapCatalogItem, level int) (mapCatalogItem, bool) {
	var candidates []mapCatalogItem
	for _, mp := range maps {
		if mp.Use && mp.Village >= 0 && mp.Area >= 0 && mp.Level <= level {
			candidates = append(candidates, mp)
		}
	}
	if len(candidates) == 0 {
		return mapCatalogItem{}, false
	}
	return candidates[m.randIntn(len(candidates))], true
}
