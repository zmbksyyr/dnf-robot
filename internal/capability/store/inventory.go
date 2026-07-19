package store

import (
	"encoding/binary"
	"strings"

	"robot/internal/shared"
)

const (
	WorldHornItemID   = 36
	WorldHornCount    = 200
	WorldHornBoxIndex = 55
	WorldHornRawIndex = WorldHornBoxIndex + 2
)

type InventoryPlan struct {
	Name     string
	StartBox int
}

type StallItem struct {
	ItemID int
	Count  int
	Price  int
}

type StallResult struct {
	StallRows  int
	ConfigRows int
}

type PermissionStatus struct {
	Premium    int
	Miles      int
	ProdUser   int
	PUUser     int
	EventEntry int
}

type ExpRepairResult struct {
	RefRows int
	Changed int64
}

func LevelMinExp(level int) (int, bool) {
	if level < 1 || level >= len(levelMinExpTable) {
		return 0, false
	}
	return levelMinExpTable[level], true
}

var levelMinExpTable = []int{
	0,
	0, 1000, 2653, 5543, 10575, 18509, 30205, 46627, 68840, 98012,
	135412, 182411, 240483, 311203, 396249, 497399, 619844, 767003, 942667, 1150141,
	1393864, 1677655, 2016592, 2419616, 2881880, 3410208, 4010357, 4690036, 5457538, 6319795,
	7286075, 8364071, 9564081, 10897068, 12372076, 14001186, 15794305, 17764684, 19926329, 22290671,
	24872971, 27685628, 30844672, 34385491, 38223728, 42379527, 46869144, 51714280, 56937632, 62557467,
	68598134, 75079161, 82244762, 90154153, 98612376, 107650320, 117292652, 127572249, 138523258, 150172894,
	162557394, 175705561, 190075639, 205759433, 222370655, 239953967, 258544759, 278190155, 298938852, 320829378,
	343912998, 368230186, 394571074, 423070174, 453854220, 487070095, 522855717, 561371046, 602783706, 647250436,
	694953116, 746061309, 801975949, 863016468, 929492724,
}

func InventoryPlanFor(startBox int) InventoryPlan {
	start := startBox
	if start <= 0 || start == 7 {
		start = 105
	}
	return InventoryPlan{Name: "material-default", StartBox: start}
}

func InventoryClearStartBoxes(start int) []int {
	if start == 105 {
		return []int{105}
	}
	return []int{start, 105}
}

func AttachAllowed(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	if attach == "" {
		return false
	}
	if strings.Contains(attach, "account") || strings.Contains(attach, "creature") || strings.Contains(attach, "unable") || strings.Contains(attach, "not") {
		return false
	}
	return strings.Contains(attach, "trade") || attach == "free" || attach == "sealing"
}

func AttachPreferred(attach string) bool {
	attach = strings.ToLower(strings.TrimSpace(attach))
	return attach == "trade" || strings.Contains(attach, "trade ") || attach == "free" || attach == "sealing"
}

func InventoryTypeForBoxIndex(boxIndex int) int {
	switch {
	case boxIndex >= 7 && boxIndex <= 54:
		return 1
	case boxIndex >= 55 && boxIndex <= 102:
		return 2
	case boxIndex >= 103 && boxIndex <= 150:
		return 3
	case boxIndex >= 151 && boxIndex <= 198:
		return 4
	case boxIndex >= 199 && boxIndex <= 246:
		return 10
	default:
		return 2
	}
}

func InventoryTypeForStackable(item shared.EquipmentCatalogItem, fallback int) int {
	switch strings.ToLower(strings.TrimSpace(item.Slot)) {
	case "waste", "usable", "consumable":
		return 2
	case "material":
		return 3
	case "quest":
		return 4
	case "profession", "expert job":
		return 10
	default:
		return fallback
	}
}

func WriteInventoryStack(dst []byte, item shared.EquipmentCatalogItem, count int, inventoryType int) {
	if len(dst) < 61 {
		return
	}
	clear(dst)
	dst[0] = 0x00
	dst[1] = byte(inventoryType)
	binary.LittleEndian.PutUint32(dst[2:6], uint32(item.ID))
	binary.LittleEndian.PutUint32(dst[7:11], uint32(count))
}
