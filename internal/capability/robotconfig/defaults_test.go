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
