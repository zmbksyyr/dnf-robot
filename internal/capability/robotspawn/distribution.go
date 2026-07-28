package robotspawn

import "robot/internal/shared"

type BalancedTarget struct {
	Map       shared.MapCatalogItem
	Rectangle shared.MapRectangle
}

type mapCandidate struct {
	mp         shared.MapCatalogItem
	rectangles []shared.MapRectangle
	weight     int
}

type rectangleKey struct {
	area  shared.MapAreaKey
	index int
}

type rectangleTargetIndex struct {
	mapIndex       int
	rectangleIndex int
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
		rectangles := MapRectangles(mp)
		if len(rectangles) == 0 {
			continue
		}
		candidates = append(candidates, mapCandidate{mp: mp, rectangles: rectangles, weight: SmoothedRectanglesWeight(rectangles)})
	}
	if len(candidates) == 0 {
		return nil, false
	}

	areaCounts := make(map[shared.MapAreaKey]int, len(candidates))
	rectangleCounts := make(map[rectangleKey]int)
	for _, location := range locations {
		area := shared.MapAreaKey{Village: location.Village, Area: location.Area}
		for _, candidate := range candidates {
			if mapAreaKey(candidate.mp) != area {
				continue
			}
			for rectangleIndex, rectangle := range candidate.rectangles {
				if RectangleContains(rectangle, location.X, location.Y) {
					areaCounts[area]++
					rectangleCounts[rectangleKey{area: area, index: rectangleIndex}]++
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
		rectangleIndex := 0
		if len(emptyAreas) > 0 {
			chosen = randomIndex(env, emptyAreas)
			candidate := candidates[chosen]
			rectangleIndex = leastLoadedRectangleIndex(env, candidate.rectangles, mapAreaKey(candidate.mp), rectangleCounts)
		} else if target, ok := randomEmptyRectangleTarget(env, candidates, eligible, rectangleCounts); ok {
			chosen = target.mapIndex
			rectangleIndex = target.rectangleIndex
		} else {
			chosen = leastLoadedMapIndex(env, candidates, eligible, areaCounts)
			candidate := candidates[chosen]
			rectangleIndex = leastLoadedRectangleIndex(env, candidate.rectangles, mapAreaKey(candidate.mp), rectangleCounts)
		}

		candidate := candidates[chosen]
		area := mapAreaKey(candidate.mp)
		areaCounts[area]++
		rectangleCounts[rectangleKey{area: area, index: rectangleIndex}]++
		out = append(out, BalancedTarget{Map: candidate.mp, Rectangle: candidate.rectangles[rectangleIndex]})
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

func randomEmptyRectangleTarget(env RangeRandom, candidates []mapCandidate, mapIndexes []int, counts map[rectangleKey]int) (rectangleTargetIndex, bool) {
	targets := make([]rectangleTargetIndex, 0)
	for _, mapIndex := range mapIndexes {
		candidate := candidates[mapIndex]
		area := mapAreaKey(candidate.mp)
		for rectangleIndex := range candidate.rectangles {
			if counts[rectangleKey{area: area, index: rectangleIndex}] == 0 {
				targets = append(targets, rectangleTargetIndex{mapIndex: mapIndex, rectangleIndex: rectangleIndex})
			}
		}
	}
	if len(targets) == 0 {
		return rectangleTargetIndex{}, false
	}
	return targets[randomIndex(env, indexes(len(targets)))], true
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

func leastLoadedRectangleIndex(env RangeRandom, rectangles []shared.MapRectangle, area shared.MapAreaKey, counts map[rectangleKey]int) int {
	all := indexes(len(rectangles))
	empty := make([]int, 0, len(rectangles))
	for _, index := range all {
		if counts[rectangleKey{area: area, index: index}] == 0 {
			empty = append(empty, index)
		}
	}
	if len(empty) > 0 {
		return randomIndex(env, empty)
	}
	best := []int{all[0]}
	for _, index := range all[1:] {
		bestIndex := best[0]
		left := counts[rectangleKey{area: area, index: index}] * SmoothedRectangleWeight(rectangles[bestIndex])
		right := counts[rectangleKey{area: area, index: bestIndex}] * SmoothedRectangleWeight(rectangles[index])
		switch {
		case left < right:
			best = []int{index}
		case left == right:
			best = append(best, index)
		}
	}
	return randomIndex(env, best)
}

func randomIndex(env RangeRandom, values []int) int {
	choice := env.RandBetween(0, len(values)-1)
	if choice < 0 || choice >= len(values) {
		choice = 0
	}
	return values[choice]
}

func indexes(count int) []int {
	out := make([]int, count)
	for index := range out {
		out[index] = index
	}
	return out
}

func mapAreaKey(mp shared.MapCatalogItem) shared.MapAreaKey {
	return shared.MapAreaKey{Village: mp.Village, Area: mp.Area}
}
