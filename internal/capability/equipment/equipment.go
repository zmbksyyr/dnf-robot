package equipment

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math/rand"
	"sort"

	robotconfig "robot/internal/capability/robotconfig"
	foundrand "robot/internal/foundation/random"
	"robot/internal/shared"
)

type SlotOptions struct {
	IntensifyMin int
	IntensifyMax int
	SmithingMin  int
	SmithingMax  int
}

func CompressedZeros(length int) []byte {
	if length < 0 {
		length = 0
	}
	return CompressRaw(make([]byte, length))
}

func CompressRaw(raw []byte) []byte {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(len(raw)))
	return blob
}

func SlotToItemType(slot int) int {
	switch slot {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 5
	case 6:
		return 6
	case 7:
		return 7
	case 8:
		return 8
	case 9:
		return 9
	case 10:
		return 10
	case 11:
		return 11
	case 12:
		return 12
	default:
		return 0
	}
}

func UsableByJob(jobs []int, job int) bool {
	if len(jobs) == 0 {
		return true
	}
	for _, j := range jobs {
		if j == 100 || j == job {
			return true
		}
	}
	return false
}

func AvatarUsableByJob(item shared.EquipmentCatalogItem, job int) bool {
	if item.ItemType < 20 || item.ItemType > 29 {
		return false
	}
	if len(item.UseJob) == 0 {
		return item.ItemType == 29
	}
	for _, j := range item.UseJob {
		if j == job {
			return true
		}
	}
	return false
}

func SafeAvg(total, count int) int {
	if count <= 0 {
		return 0
	}
	return total / count
}

func SelectEquipment(items []shared.EquipmentCatalogItem, level int, job int, rc robotconfig.RuntimeConfig, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	slots := rc.EquipSlots
	if len(slots) == 0 {
		slots = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	}
	candidatesBySlot := make(map[int][]shared.EquipmentCatalogItem)
	for _, slot := range slots {
		itemType := SlotToItemType(slot)
		if itemType == 0 {
			continue
		}
		var candidates []shared.EquipmentCatalogItem
		bestLevel := 0
		for _, item := range items {
			if item.ID == 0 || item.Expire || item.ItemType != itemType || item.Level > level {
				continue
			}
			if rc.EquipRarityMax > 0 && (item.Rarity < rc.EquipRarityMin || item.Rarity > rc.EquipRarityMax) {
				continue
			}
			if !UsableByJob(item.UseJob, job) {
				continue
			}
			if item.Level > bestLevel {
				bestLevel = item.Level
			}
			candidates = append(candidates, item)
		}
		if len(candidates) == 0 {
			continue
		}
		if bestLevel > 0 {
			var near []shared.EquipmentCatalogItem
			for _, item := range candidates {
				if item.Level >= bestLevel-10 {
					near = append(near, item)
				}
			}
			if len(near) > 0 {
				candidates = near
			}
		}
		candidatesBySlot[slot] = candidates
	}
	selected := make(map[int]shared.EquipmentCatalogItem)
	if rc.PreferEquipSets {
		selected = SelectSetItems(candidatesBySlot, rc.EquipSetMinSlots, randIntn)
	}
	FillRandomItems(selected, candidatesBySlot, randIntn)
	return selected
}

func SelectAvatar(items []shared.EquipmentCatalogItem, job int, rc robotconfig.RuntimeConfig, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	slots := rc.AvatarSlots
	if len(slots) == 0 {
		slots = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	}
	candidatesBySlot := make(map[int][]shared.EquipmentCatalogItem)
	for _, slot := range slots {
		itemType := slot + 20
		var candidates []shared.EquipmentCatalogItem
		for _, item := range items {
			if item.ID == 0 || item.Expire || item.ItemType != itemType {
				continue
			}
			if !AvatarUsableByJob(item, job) {
				continue
			}
			candidates = append(candidates, item)
		}
		candidatesBySlot[slot] = candidates
	}
	selected := make(map[int]shared.EquipmentCatalogItem)
	if rc.PreferAvatarSets {
		selected = SelectAvatarSetItems(candidatesBySlot, rc.AvatarSetMinSlots, randIntn)
	}
	FillRandomItems(selected, candidatesBySlot, randIntn)
	return selected
}

func BuildEquipmentSlots(items []shared.EquipmentCatalogItem, level int, job int, rc robotconfig.RuntimeConfig, randIntn func(int) int, withRand func(func(*rand.Rand)) error) []byte {
	selected := SelectEquipment(items, level, job, rc, randIntn)
	raw := make([]byte, 12*61)
	for slot, item := range selected {
		write := func(rng *rand.Rand) {
			WriteEquipSlot(raw[(slot-1)*61:slot*61], item, rng, SlotOptions{
				IntensifyMin: rc.EquipIntensifyMin,
				IntensifyMax: rc.EquipIntensifyMax,
				SmithingMin:  rc.EquipSmithingMin,
				SmithingMax:  rc.EquipSmithingMax,
			})
		}
		if withRand != nil {
			_ = withRand(write)
		} else {
			write(rand.New(rand.NewSource(1)))
		}
	}
	return raw
}

type setGroup struct {
	key       string
	bySlot    map[int][]shared.EquipmentCatalogItem
	coverage  int
	levelSum  int
	raritySum int
	count     int
}

func SelectSetItems(candidatesBySlot map[int][]shared.EquipmentCatalogItem, minSlots int, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	return selectBestSetItems(buildSetGroups(candidatesBySlot), minSlots, randIntn)
}

func SelectAvatarSetItems(candidatesBySlot map[int][]shared.EquipmentCatalogItem, minSlots int, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	groups := buildSetGroups(candidatesBySlot)
	coverageFloor := 6
	if minSlots > coverageFloor {
		coverageFloor = minSlots
	}
	eligible := make([]*setGroup, 0, len(groups))
	for _, group := range groups {
		if group.coverage >= coverageFloor {
			eligible = append(eligible, group)
		}
	}
	if len(eligible) == 0 {
		return selectBestSetItems(groups, minSlots, randIntn)
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].key < eligible[j].key })
	return selectSetGroup(eligible[safeRandIntn(randIntn, len(eligible))], randIntn)
}

func buildSetGroups(candidatesBySlot map[int][]shared.EquipmentCatalogItem) map[string]*setGroup {
	groups := make(map[string]*setGroup)
	for slot, candidates := range candidatesBySlot {
		for _, item := range candidates {
			if item.SetKey == "" {
				continue
			}
			group := groups[item.SetKey]
			if group == nil {
				group = &setGroup{key: item.SetKey, bySlot: make(map[int][]shared.EquipmentCatalogItem)}
				groups[item.SetKey] = group
			}
			if len(group.bySlot[slot]) == 0 {
				group.coverage++
			}
			group.bySlot[slot] = append(group.bySlot[slot], item)
			group.levelSum += item.Level
			group.raritySum += item.Rarity
			group.count++
		}
	}
	return groups
}

func selectBestSetItems(groups map[string]*setGroup, minSlots int, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	if minSlots <= 1 {
		minSlots = 2
	}
	var best []*setGroup
	bestScore := -1
	for _, group := range groups {
		if group.coverage < minSlots {
			continue
		}
		score := group.coverage*1000000 + SafeAvg(group.levelSum, group.count)*1000 + SafeAvg(group.raritySum, group.count)
		if score > bestScore {
			bestScore = score
			best = []*setGroup{group}
		} else if score == bestScore {
			best = append(best, group)
		}
	}
	selected := make(map[int]shared.EquipmentCatalogItem)
	if len(best) == 0 {
		return selected
	}
	return selectSetGroup(best[safeRandIntn(randIntn, len(best))], randIntn)
}

func selectSetGroup(group *setGroup, randIntn func(int) int) map[int]shared.EquipmentCatalogItem {
	selected := make(map[int]shared.EquipmentCatalogItem)
	if group == nil {
		return selected
	}
	for slot, candidates := range group.bySlot {
		if len(candidates) == 0 {
			continue
		}
		selected[slot] = candidates[safeRandIntn(randIntn, len(candidates))]
	}
	return selected
}

func FillRandomItems(selected map[int]shared.EquipmentCatalogItem, candidatesBySlot map[int][]shared.EquipmentCatalogItem, randIntn func(int) int) {
	for slot, candidates := range candidatesBySlot {
		if _, ok := selected[slot]; ok || len(candidates) == 0 {
			continue
		}
		selected[slot] = candidates[safeRandIntn(randIntn, len(candidates))]
	}
}

func WriteEquipSlot(dst []byte, item shared.EquipmentCatalogItem, rng *rand.Rand, opt SlotOptions) {
	if len(dst) < 61 {
		return
	}
	dst[0] = 0x00
	dst[1] = 0x01
	binary.LittleEndian.PutUint32(dst[2:6], uint32(item.ID))
	intensifyMin := maxInt(opt.IntensifyMin, 7)
	intensifyMax := maxInt(opt.IntensifyMax, intensifyMin)
	intensify := foundrand.BetweenAtLeast(rng, intensifyMin, intensifyMax)
	if item.ItemType == 1 {
		intensify = foundrand.BetweenAtLeast(rng, 8, 15)
	}
	if item.ItemType == 2 {
		intensify = 0
	}
	dst[6] = byte(intensify)
	binary.LittleEndian.PutUint32(dst[7:11], uint32(foundrand.BetweenAtLeast(rng, 0, 400000)))
	dst[11] = byte(foundrand.BetweenAtLeast(rng, 10, 30))
	if item.ItemType == 1 {
		dst[51] = byte(foundrand.BetweenAtLeast(rng, opt.SmithingMin, opt.SmithingMax))
	}
}

func safeRandIntn(randIntn func(int) int, n int) int {
	if n <= 0 || randIntn == nil {
		return 0
	}
	v := randIntn(n)
	if v < 0 || v >= n {
		return 0
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
