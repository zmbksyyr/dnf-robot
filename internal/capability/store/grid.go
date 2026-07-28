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
		if !IsStoreMapEligible(mp) {
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

func FilterEligibleGridPoints(points []GridPoint, maps []shared.MapCatalogItem) []GridPoint {
	if len(points) == 0 {
		return nil
	}
	eligible := make(map[areaKey]bool)
	for _, mp := range maps {
		if IsStoreMapEligible(mp) {
			eligible[areaKey{mp.Village, mp.Area}] = true
		}
	}
	out := points[:0]
	for _, pt := range points {
		if !eligible[areaKey{pt.Village, pt.Area}] || pt.X <= 0 || pt.Y <= 0 {
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

func IsNormalMapEligible(mp shared.MapCatalogItem) bool {
	if mp.NormalEligible != nil {
		return mp.Use && *mp.NormalEligible
	}
	return mp.Use
}

func IsStoreMapEligible(mp shared.MapCatalogItem) bool {
	if mp.StoreEligible != nil {
		return mp.Use && *mp.StoreEligible
	}
	return mp.Use && !mp.Gate
}
