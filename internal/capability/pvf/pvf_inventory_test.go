package pvf

import (
	"os"
	"path/filepath"
	"testing"

	"robot/internal/shared"
)

func TestInventoryKindsLoadThroughPVFLifecycle(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { shared.SetInventoryItemKinds(nil, nil) })
	if err := WriteJSON(filepath.Join(dir, pvfEquipmentExportName), []shared.EquipmentCatalogItem{{ID: 27600}, {ID: 999}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(dir, pvfStackableExportName), []shared.EquipmentCatalogItem{{ID: 4183}, {ID: 999}}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", filepath.Join(dir, "missing", "df_game_r")} {
		if err := EnsureExports(source, dir); err != nil {
			t.Fatal(err)
		}
		if shared.InventoryItemKindForID(27600) != shared.InventoryItemEquipment || shared.InventoryItemKindForID(4183) != shared.InventoryItemStackable || shared.InventoryItemKindForID(999) != shared.InventoryItemUnknown {
			t.Fatal("catalog lifecycle lost identities or allowed conflicting definitions")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, pvfStackableExportName), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExports("", dir); err != nil {
		t.Fatal(err)
	}
	if shared.InventoryItemKindForID(27600) != shared.InventoryItemUnknown {
		t.Fatal("malformed reload retained previous identities")
	}
}
