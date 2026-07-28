package robotspawn

import "robot/internal/shared"

type BalancedTarget struct {
	Map shared.MapCatalogItem
	X   int
	Y   int
}

type mapCandidate struct {
	mp         shared.MapCatalogItem
	rectangles []shared.MapRectangle
	weight     int
}

func DistributedTargets(env RangeRandom, maps []shared.MapCatalogItem, levels []int, locations []shared.MapLocation) ([]BalancedTarget, bool) {
	if env == nil || len(levels) == 0 {
		return nil, false
	}
	candidates := make([]mapCandidate, 0, len(maps))
	for _, mp := range maps {
		if !mp.Use || mp.Village < 0 || mp.Area < 0 {
			continue
		}
		rectangles := NormalizeRectangles(MapRectangles(mp))
		if len(rectangles) == 0 {
			continue
		}
		candidates = append(candidates, mapCandidate{mp: mp, rectangles: rectangles, weight: SmoothedRectanglesWeight(rectangles)})
	}
	if len(candidates) == 0 {
		return nil, false
	}

	areaCounts := make(map[shared.MapAreaKey]int, len(candidates))
	areaLocations := make(map[shared.MapAreaKey][]shared.MapLocation, len(candidates))
	for _, location := range locations {
		area := shared.MapAreaKey{Village: location.Village, Area: location.Area}
		for _, candidate := range candidates {
			if mapAreaKey(candidate.mp) != area {
				continue
			}
			for _, rectangle := range candidate.rectangles {
				if RectangleContains(rectangle, location.X, location.Y) {
					areaCounts[area]++
					areaLocations[area] = append(areaLocations[area], location)
					break
				}
			}
			break
		}
	}

	out := make([]BalancedTarget, 0, len(levels))
	for _, level := range levels {
		emptyAreas := eligibleMapIndexes(candidates, areaCounts, level, true)
		eligible := emptyAreas
		if len(emptyAreas) == 0 {
			eligible = eligibleMapIndexes(candidates, areaCounts, level, false)
		}
		if len(eligible) == 0 {
			out = append(out, BalancedTarget{})
			continue
		}

		chosen := 0
		if len(emptyAreas) > 0 {
			chosen = randomIndex(env, emptyAreas)
		} else {
			chosen = leastLoadedMapIndex(env, candidates, eligible, areaCounts)
		}

		candidate := candidates[chosen]
		area := mapAreaKey(candidate.mp)
		x, y, pointOK := bestRandomPoint(env, candidate.rectangles, areaLocations[area])
		if !pointOK {
			out = append(out, BalancedTarget{})
			continue
		}
		areaCounts[area]++
		location := shared.MapLocation{Village: candidate.mp.Village, Area: candidate.mp.Area, X: x, Y: y}
		areaLocations[area] = append(areaLocations[area], location)
		out = append(out, BalancedTarget{Map: candidate.mp, X: x, Y: y})
	}
	return out, true
}

func BalancedLocation(env RangeRandom, maps []shared.MapCatalogItem, level int, locations []shared.MapLocation) (BalancedTarget, bool) {
	targets, ok := DistributedTargets(env, maps, []int{level}, locations)
	if !ok || len(targets) == 0 || !targets[0].Map.Use {
		return BalancedTarget{}, false
	}
	return targets[0], true
}

func eligibleMapIndexes(candidates []mapCandidate, counts map[shared.MapAreaKey]int, level int, emptyOnly bool) []int {
	eligible := make([]int, 0, len(candidates))
	for index, candidate := range candidates {
		if candidate.mp.Level > level {
			continue
		}
		if emptyOnly && counts[mapAreaKey(candidate.mp)] > 0 {
			continue
		}
		eligible = append(eligible, index)
	}
	return eligible
}

func leastLoadedMapIndex(env RangeRandom, candidates []mapCandidate, indexes []int, counts map[shared.MapAreaKey]int) int {
	best := []int{indexes[0]}
	for _, index := range indexes[1:] {
		bestIndex := best[0]
		left := counts[mapAreaKey(candidates[index].mp)] * candidates[bestIndex].weight
		right := counts[mapAreaKey(candidates[bestIndex].mp)] * candidates[index].weight
		switch {
		case left < right:
			best = []int{index}
		case left == right:
			best = append(best, index)
		}
	}
	return randomIndex(env, best)
}

func bestRandomPoint(env RangeRandom, rectangles []shared.MapRectangle, occupied []shared.MapLocation) (int, int, bool) {
	candidateCount := 1
	if len(occupied) > 0 {
		candidateCount = 8
	}
	bestX, bestY := 0, 0
	bestDistance := int64(-1)
	for index := 0; index < candidateCount; index++ {
		x, y, ok := randomPointFromNormalized(env, rectangles)
		if !ok {
			return 0, 0, false
		}
		distance := nearestDistanceSquared(x, y, occupied)
		if distance > bestDistance {
			bestX, bestY, bestDistance = x, y, distance
		}
	}
	return bestX, bestY, true
}

func nearestDistanceSquared(x, y int, occupied []shared.MapLocation) int64 {
	if len(occupied) == 0 {
		return 0
	}
	best := int64(^uint64(0) >> 1)
	for _, location := range occupied {
		dx := int64(x - location.X)
		dy := int64(y - location.Y)
		distance := dx*dx + dy*dy
		if distance < best {
			best = distance
		}
	}
	return best
}

func randomIndex(env RangeRandom, values []int) int {
	choice := env.RandBetween(0, len(values)-1)
	if choice < 0 || choice >= len(values) {
		choice = 0
	}
	return values[choice]
}

func mapAreaKey(mp shared.MapCatalogItem) shared.MapAreaKey {
	return shared.MapAreaKey{Village: mp.Village, Area: mp.Area}
}
