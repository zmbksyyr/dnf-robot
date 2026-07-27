package store

import (
	"sort"
	"time"
)

func (c *PointCoordinator) rebuildPackedPointsLocked() {
	c.packedPoints = buildPackedPointSet(c.points)
}

func buildPackedPointSet(points []GridPoint) map[string]bool {
	type packedCell struct {
		area areaKey
		cell occupancyCell
	}

	packed := make(map[string]bool, len(points)/2)
	selected := make(map[packedCell]GridPoint, len(points)/2)
	for _, point := range points {
		if _, permanent := pointFailureRetry(point.LastReason, pointClaimTTL); permanent {
			continue
		}
		area := areaKey{point.Village, point.Area}
		cell := occupancyCellFor(point.X, point.Y)
		conflict := false
		for y := cell.Y - 1; y <= cell.Y+1 && !conflict; y++ {
			for x := cell.X - 1; x <= cell.X+1 && !conflict; x++ {
				existing, ok := selected[packedCell{area: area, cell: occupancyCell{X: x, Y: y}}]
				if ok && positionsOccupancyConflict(point, existing) {
					conflict = true
				}
			}
		}
		if conflict {
			continue
		}
		packed[point.ID] = true
		selected[packedCell{area: area, cell: cell}] = point
	}
	return packed
}

type pointFailureObservation struct {
	index int
	at    time.Time
}

func normalizeAmbiguousFailureBursts(points []GridPoint) int {
	type failureKey struct {
		uid    int
		reason string
	}
	groups := make(map[failureKey][]pointFailureObservation)
	for index := range points {
		pt := points[index]
		if pt.LastUID <= 0 || !ambiguousPointFailureReason(pt.LastReason) || pt.LastResultAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, pt.LastResultAt)
		if err != nil {
			continue
		}
		key := failureKey{uid: pt.LastUID, reason: pt.LastReason}
		groups[key] = append(groups[key], pointFailureObservation{index: index, at: at})
	}

	pruned := 0
	for _, observations := range groups {
		sort.Slice(observations, func(i, j int) bool { return observations[i].at.Before(observations[j].at) })
		for start := 0; start < len(observations); {
			end := start + 1
			for end < len(observations) && observations[end].at.Sub(observations[start].at) <= pointFailureBurst {
				end++
			}
			pruned += normalizeAmbiguousFailureBurst(points, observations[start:end])
			start = end
		}
	}
	return pruned
}

func normalizeAmbiguousFailureBurst(points []GridPoint, observations []pointFailureObservation) int {
	if len(observations) < 2 {
		return 0
	}
	independent := false
	for i := 0; i < len(observations) && !independent; i++ {
		for j := i + 1; j < len(observations); j++ {
			if !positionsConflict(points[observations[i].index], points[observations[j].index]) {
				independent = true
				break
			}
		}
	}
	start := 1
	if independent {
		start = 0
	}
	for _, observation := range observations[start:] {
		clearAmbiguousPointFailure(&points[observation.index])
	}
	return len(observations) - start
}

func clearAmbiguousPointFailure(point *GridPoint) {
	if point == nil {
		return
	}
	if point.Failed > 0 {
		point.Failed--
	}
	point.LastUID = 0
	point.LastReason = ""
	point.LastResultAt = ""
	if point.Success > 0 {
		point.Status = PointStatusSuccess
	} else {
		point.Status = PointStatusUnknown
	}
}
