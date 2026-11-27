package pvf

import (
	"encoding/json"
	"os"
	"path/filepath"

	"robot/internal/shared"
)

func loadInventoryItemKinds(pvfDir string) error {
	shared.SetInventoryItemKinds(nil, nil)
	var catalogs [2][]shared.EquipmentCatalogItem
	for i, name := range []string{pvfEquipmentExportName, pvfStackableExportName} {
		data, err := os.ReadFile(filepath.Join(pvfDir, name))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &catalogs[i]); err != nil {
			return err
		}
	}
	shared.SetInventoryItemKinds(catalogs[0], catalogs[1])
	return nil
}
