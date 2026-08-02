package robotconfig

import "testing"

func TestNormalizeKeepsPVFDefinedVillageAboveLegacyRange(t *testing.T) {
	rc := Default()
	rc.SpawnVillage = 26
	Normalize(&rc)
	if rc.SpawnVillage != 26 {
		t.Fatalf("spawn village=%d, want 26", rc.SpawnVillage)
	}
}

func TestNormalizeStoreEquipmentIntensifyRange(t *testing.T) {
	rc := Default()
	rc.StoreEquipmentIntensifyMin = 40
	rc.StoreEquipmentIntensifyMax = 6
	Normalize(&rc)
	if rc.StoreEquipmentIntensifyMin != 6 || rc.StoreEquipmentIntensifyMax != 31 {
		t.Fatalf("store equipment intensify=%d..%d, want 6..31", rc.StoreEquipmentIntensifyMin, rc.StoreEquipmentIntensifyMax)
	}
}
