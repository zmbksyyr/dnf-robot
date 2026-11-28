package shared

import "sync/atomic"

// EquipmentResources retains the resource fields used by character login and
// skill payment. Values use the equipment script's units.
type EquipmentResources struct {
	HP       int                    `json:"hp,omitempty"`
	MP       int                    `json:"mp,omitempty"`
	Vitality int                    `json:"vitality,omitempty"`
	Spirit   int                    `json:"spirit,omitempty"`
	HPRate   float32                `json:"hp_rate,omitempty"`
	MPRate   float32                `json:"mp_rate,omitempty"`
	Options  []ResourceStats        `json:"options,omitempty"`
	Sets     []EquipmentResourceSet `json:"sets,omitempty"`
}

type ResourceStats struct {
	HP       int     `json:"hp,omitempty"`
	MP       int     `json:"mp,omitempty"`
	Vitality int     `json:"vitality,omitempty"`
	Spirit   int     `json:"spirit,omitempty"`
	HPRate   float32 `json:"hp_rate,omitempty"`
	MPRate   float32 `json:"mp_rate,omitempty"`
}

type EquipmentResourceSet struct {
	Items    []int `json:"items"`
	Pieces   int   `json:"pieces"`
	HP       int   `json:"hp,omitempty"`
	MP       int   `json:"mp,omitempty"`
	Vitality int   `json:"vitality,omitempty"`
	Spirit   int   `json:"spirit,omitempty"`
}

var resourceEquipmentSnapshot atomic.Pointer[map[int]EquipmentCatalogItem]

func SetResourceEquipment(items []EquipmentCatalogItem) {
	index := make(map[int]EquipmentCatalogItem, len(items))
	for _, item := range items {
		index[item.ID] = item
	}
	resourceEquipmentSnapshot.Store(&index)
	characterResourceGeneration.Add(1)
}

func ResourceEquipment(id int) (EquipmentCatalogItem, bool) {
	index := resourceEquipmentSnapshot.Load()
	if index == nil {
		return EquipmentCatalogItem{}, false
	}
	item, ok := (*index)[id]
	return item, ok
}
