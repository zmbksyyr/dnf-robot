package store

import (
	"fmt"
	"sort"

	robotspawn "robot/internal/capability/robotspawn"
	"robot/internal/shared"
)

const (
	PointCacheFile = "store_points_cache.json"
	PointCacheVer  = 1
	PointXStep     = 120
	PointYStep     = 80
	RestrictHalfX  = 80
	RestrictHalfY  = 150
)

type GridPoint struct {
	ID           string `json:"id"`
	Village      int    `json:"village"`
	Area         int    `json:"area"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Status       string `json:"status"`
	Success      int    `json:"success"`
	Failed       int    `json:"failed"`
	LastUID      int    `json:"last_uid,omitempty"`
	LastReason   string `json:"last_reason,omitempty"`
	LastResultAt string `json:"last_result_at,omitempty"`
}

type PointCache struct {
	Version    int         `json:"version"`
	SourceFile string      `json:"source_file"`
	SourceMD5  string      `json:"source_md5"`
	XStep      int         `json:"x_step"`
	YStep      int         `json:"y_step"`
	Generated  string      `json:"generated_at"`
	Updated    string      `json:"updated_at,omitempty"`
	Points     []GridPoint `json:"points"`
}

type areaKey [2]int

func BuildGridPoints(maps []shared.MapCatalogItem) []GridPoint {
	var points []GridPoint
	seen := make(map[string]struct{})
	for _, mp := range maps {
		if !mp.Use {
			continue
		}
		if mp.Gate || !IsStoreAreaEligible(mp.Village, mp.Area) {
			continue
		}
		for _, rectangle := range robotspawn.MapRectangles(mp) {
			for y := PointYStart(rectangle); y <= rectangle.YMax; y += PointYStep {
				for x := rectangle.XMin; x <= rectangle.XMax; x += PointXStep {
					if x <= 0 || y <= 0 {
						continue
					}
					id := fmt.Sprintf("%d-%d-%d-%d", mp.Village, mp.Area, x, y)
					if _, exists := seen[id]; exists {
						continue
					}
					seen[id] = struct{}{}
					points = append(points, GridPoint{
						ID: id, Village: mp.Village, Area: mp.Area, X: x, Y: y, Status: PointStatusUnknown,
					})
				}
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.Village != b.Village {
			return a.Village < b.Village
		}
		if a.Area != b.Area {
			return a.Area < b.Area
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return points
}

func FilterEligibleGridPoints(points []GridPoint) []GridPoint {
	if len(points) == 0 {
		return nil
	}
	out := points[:0]
	for _, pt := range points {
		if !IsStoreAreaEligible(pt.Village, pt.Area) || pt.X <= 0 || pt.Y <= 0 {
			continue
		}
		out = append(out, pt)
	}
	return out
}

func PointYStart(rectangle shared.MapRectangle) int {
	if rectangle.YMax <= rectangle.YMin {
		return rectangle.YMin
	}
	return rectangle.YMin + (rectangle.YMax-rectangle.YMin)/2
}

func IsNormalAreaEligible(village, area int) bool {
	key := areaKey{village, area}
	return GateArea[key] || AreaEligible[key]
}

func CanMoveToStoreArea(fromVillage, fromArea, toVillage, toArea int) bool {
	return IsNormalAreaEligible(fromVillage, fromArea) && IsStoreAreaEligible(toVillage, toArea)
}

func IsStoreAreaEligible(village, area int) bool {
	key := areaKey{village, area}
	if GateArea[key] {
		return false
	}
	return AreaEligible[key]
}

var GateArea = map[areaKey]bool{
	{1, 1}: true, {2, 5}: true, {3, 2}: true, {4, 1}: true, {5, 1}: true,
	{6, 4}: true, {8, 1}: true, {9, 2}: true, {10, 1}: true, {11, 3}: true,
	{14, 3}: true, {15, 0}: true, {16, 0}: true, {17, 0}: true, {18, 0}: true,
	{19, 0}: true, {20, 0}: true, {21, 7}: true, {23, 0}: true, {24, 0}: true,
	{25, 0}: true, {26, 0}: true,
}

var AreaEligible = map[areaKey]bool{
	{1, 0}: true,
	{2, 0}: true, {2, 1}: true, {2, 2}: true, {2, 8}: true,
	{3, 0}: true, {3, 8}: true,
	{4, 0}: true, {4, 5}: true,
	{5, 0}: true,
	{6, 0}: true, {6, 1}: true,
	{8, 0}:  true,
	{9, 0}:  true,
	{10, 2}: true, {10, 3}: true,
	{14, 4}: true, {14, 5}: true,
	{15, 1}: true, {15, 3}: true,
	{16, 1}: true,
	{17, 1}: true, {17, 3}: true, {17, 4}: true,
	{18, 1}: true,
	{19, 1}: true, {19, 3}: true,
	{20, 1}: true, {20, 2}: true,
	{21, 0}: true, {21, 1}: true, {21, 2}: true, {21, 3}: true,
	{23, 1}: true, {23, 3}: true, {23, 4}: true,
	{24, 1}: true,
	{25, 1}: true,
}

var AreaPriority = map[areaKey]int{
	{4, 0}:  30,
	{5, 0}:  26,
	{6, 0}:  17,
	{2, 8}:  17,
	{2, 0}:  16,
	{2, 1}:  15,
	{8, 0}:  13,
	{3, 8}:  11,
	{9, 0}:  10,
	{3, 0}:  9,
	{2, 2}:  9,
	{10, 2}: 7,
	{1, 0}:  6,
	{6, 1}:  4,
	{25, 1}: 4,
	{2, 3}:  4,
	{4, 5}:  3,
	{11, 1}: 3,
	{23, 3}: 2,
	{16, 1}: 2,
	{11, 0}: 2,
}
