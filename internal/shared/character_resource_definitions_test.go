package shared

import (
	"math"
	"testing"
)

func TestCharacterResourceDefinitionsPublishOwnedRowsAndNativeColumns(t *testing.T) {
	t.Cleanup(func() { _ = SetCharacterResourceDefinitions(nil) })
	definitions := CharacterResourceDefinitions{Version: 1, TitlebookDefaultKnown: true,
		TitlebookDefault: []ResourceEffect{{Stat: 13, Percent: true, Value: 100}, {Stat: 3, Percent: true, Value: -90}},
		Weapons:          []CharacterWeaponResources{{Job: 8, SkillInfo: [][4]float32{{1, 2, 3, 4}, {.75, .5, 1.25, 1.5}}}}}
	if err := SetCharacterResourceDefinitions(&definitions); err != nil {
		t.Fatal(err)
	}
	definitions.Weapons[0].SkillInfo[1][0] = 99
	for _, tc := range []struct {
		effect byte
		want   float32
	}{{1, .75}, {2, 1.25}} {
		if got, known := CharacterWeaponMPRate(8, 1, tc.effect); !known || got != tc.want {
			t.Fatalf("weapon MP column effect=%d got=%g known=%t", tc.effect, got, known)
		}
	}
	bonus, known := TitlebookDefaultResourceBonus()
	if !known || bonus.HPRate != 1 || float32(1+bonus.VitalityRate) != math.Float32frombits(1036831952) {
		t.Fatalf("native titlebook float32 terms = %+v known=%t", bonus, known)
	}
	for _, args := range [][3]int{{0, 1, 1}, {8, -1, 1}, {8, 2, 1}, {8, 1, 0}, {8, 1, 3}} {
		if _, known := CharacterWeaponMPRate(args[0], args[1], byte(args[2])); known {
			t.Fatalf("missing weapon definition became known: %v", args)
		}
	}
	generation := CharacterResourceGeneration()
	if err := SetCharacterResourceDefinitions(&CharacterResourceDefinitions{Version: 2}); err == nil || CharacterResourceGeneration() != generation {
		t.Fatal("invalid catalog changed the published generation")
	}
	_ = SetCharacterResourceDefinitions(nil)
	if _, known := TitlebookDefaultResourceBonus(); known {
		t.Fatal("cleared titlebook default remained known")
	}
	if _, known := CharacterWeaponMPRate(8, 1, 1); known {
		t.Fatal("cleared weapon definition remained known")
	}
}
