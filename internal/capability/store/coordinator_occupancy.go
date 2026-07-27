package store

import (
	"sort"
	"time"

	"robot/internal/foundation/mathx"
)

const (
	// Adjacent generated columns are 120 units apart. Vertically, keep one row
	// between active stalls: 80-unit neighbors collide, while 160-unit rows
	// preserve the denser layout proven by live capacity tests.
	pointOccupancyConflictX = PointXStep - 1
	pointOccupancyConflictY = PointYStep*2 - 1
	pointOccupancyCellX     = pointOccupancyConflictX + 1
	pointOccupancyCellY     = pointOccupancyConflictY + 1
)

type occupancyCell struct {
	X int
	Y int
}

type pointOccupancy struct {
	ClaimUntil   time.Time
	SuccessUntil time.Time
}

func (c *PointCoordinator) positionRecentlyOccupied(area areaKey, candidate GridPoint, now time.Time) bool {
	cell := occupancyCellFor(candidate.X, candidate.Y)
	occupiedArea := c.pointOccupancy[area]
	for y := cell.Y - 1; y <= cell.Y+1; y++ {
		for x := cell.X - 1; x <= cell.X+1; x++ {
			for pointID, occupied := range occupiedArea[occupancyCell{X: x, Y: y}] {
				if !now.Before(occupied.ClaimUntil) && !now.Before(occupied.SuccessUntil) {
					continue
				}
				idx, ok := c.byID[pointID]
				if ok && positionsOccupancyConflict(candidate, c.points[idx]) {
					return true
				}
			}
		}
	}
	return false
}

func occupancyCellFor(x, y int) occupancyCell {
	return occupancyCell{
		X: floorDiv(x, pointOccupancyCellX),
		Y: floorDiv(y, pointOccupancyCellY),
	}
}

func floorDiv(value, divisor int) int {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}

func (c *PointCoordinator) setPointClaimLocked(pointID string, claim pointClaim) {
	c.pointClaims[pointID] = claim
	c.updatePointOccupancyLocked(pointID, true, func(occupied pointOccupancy) pointOccupancy {
		occupied.ClaimUntil = claim.ClaimUntil
		return occupied
	})
}

func (c *PointCoordinator) clearPointClaimLocked(pointID string) {
	delete(c.pointClaims, pointID)
	c.updatePointOccupancyLocked(pointID, false, func(occupied pointOccupancy) pointOccupancy {
		occupied.ClaimUntil = time.Time{}
		return occupied
	})
}

func (c *PointCoordinator) setPointSuccessOccupancyLocked(pointID string, reusableAt time.Time) {
	c.updatePointOccupancyLocked(pointID, true, func(occupied pointOccupancy) pointOccupancy {
		occupied.SuccessUntil = reusableAt
		return occupied
	})
}

func (c *PointCoordinator) clearPointSuccessOccupancyLocked(pointID string) bool {
	cleared := false
	c.updatePointOccupancyLocked(pointID, false, func(occupied pointOccupancy) pointOccupancy {
		cleared = !occupied.SuccessUntil.IsZero()
		occupied.SuccessUntil = time.Time{}
		return occupied
	})
	return cleared
}

func (c *PointCoordinator) activeOccupanciesLocked(now time.Time) []ActivePointOccupancy {
	var active []ActivePointOccupancy
	for _, cells := range c.pointOccupancy {
		for _, points := range cells {
			for pointID, occupied := range points {
				if !occupied.SuccessUntil.After(now) {
					continue
				}
				active = append(active, ActivePointOccupancy{
					PointID: pointID,
					Until:   occupied.SuccessUntil.Format(time.RFC3339Nano),
				})
			}
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].PointID < active[j].PointID
	})
	return active
}

func (c *PointCoordinator) restoreActiveOccupanciesLocked(active []ActivePointOccupancy, now time.Time) int {
	restored := 0
	for _, occupied := range active {
		if _, ok := c.byID[occupied.PointID]; !ok {
			continue
		}
		until, err := time.Parse(time.RFC3339Nano, occupied.Until)
		if err != nil || !until.After(now) {
			continue
		}
		c.setPointSuccessOccupancyLocked(occupied.PointID, until)
		restored++
	}
	return restored
}

func (c *PointCoordinator) updatePointOccupancyLocked(pointID string, create bool, update func(pointOccupancy) pointOccupancy) {
	idx, ok := c.byID[pointID]
	if !ok {
		return
	}
	point := c.points[idx]
	areaKey := areaKey{point.Village, point.Area}
	cell := occupancyCellFor(point.X, point.Y)
	area := c.pointOccupancy[areaKey]
	if area == nil {
		if !create {
			return
		}
		area = make(map[occupancyCell]map[string]pointOccupancy)
		c.pointOccupancy[areaKey] = area
	}
	points := area[cell]
	if points == nil {
		if !create {
			return
		}
		points = make(map[string]pointOccupancy)
		area[cell] = points
	}
	occupied := update(points[pointID])
	if !occupied.ClaimUntil.IsZero() || !occupied.SuccessUntil.IsZero() {
		points[pointID] = occupied
		return
	}
	delete(points, pointID)
	if len(points) == 0 {
		delete(area, cell)
	}
	if len(area) == 0 {
		delete(c.pointOccupancy, areaKey)
	}
}

func positionsConflict(first, second GridPoint) bool {
	return first.Village == second.Village && first.Area == second.Area &&
		mathx.AbsInt(first.X-second.X) <= pointOccupancyConflictX &&
		mathx.AbsInt(first.Y-second.Y) <= pointOccupancyConflictY
}

func positionsOccupancyConflict(first, second GridPoint) bool {
	return positionsConflict(first, second)
}

func positionsConflictPosition(first, second Position) bool {
	return first.Village == second.Village && first.Area == second.Area &&
		mathx.AbsInt(first.X-second.X) <= pointOccupancyConflictX &&
		mathx.AbsInt(first.Y-second.Y) <= pointOccupancyConflictY
}
