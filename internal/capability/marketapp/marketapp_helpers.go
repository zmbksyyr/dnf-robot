package marketapp

import "math/rand"

func idSet(ids []uint32) map[uint32]bool {
	out := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			out[id] = true
		}
	}
	return out
}

func filterQueueBySet(ids []uint32, keep map[uint32]bool) []uint32 {
	out := ids[:0]
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && keep[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func filterQueueExcludeSet(ids []uint32, exclude map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(ids))
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id != 0 && !exclude[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func removeQueueID(ids []uint32, itemID uint32) []uint32 {
	out := ids[:0]
	for _, id := range ids {
		if id != itemID {
			out = append(out, id)
		}
	}
	return out
}

func queueContains(ids []uint32, itemID uint32) bool {
	for _, id := range ids {
		if id == itemID {
			return true
		}
	}
	return false
}

func auctionTargetRecords(row restockRow) int {
	item := row.marketItem()
	stackSize := auctionPlanStackSize(row, item, item.Kind == "equipment")
	return (row.Quantity + stackSize - 1) / stackSize
}

func randRange(rng *rand.Rand, min, max int) int {
	if min <= 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min + rng.Intn(max-min+1)
}

func mergeStringMap(dst *map[string]string, defaults map[string]string) {
	if *dst == nil {
		*dst = map[string]string{}
	}
	for key, value := range defaults {
		(*dst)[key] = value
	}
}
