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
