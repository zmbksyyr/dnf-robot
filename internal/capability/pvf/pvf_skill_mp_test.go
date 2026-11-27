package pvf

import (
	"reflect"
	"testing"
)

func TestDungeonSkillMPUsesModeAndDistinguishesFreeFromUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         []int
	}{
		{"missing", "[cool time]\n0 0", nil},
		{"explicit-free", "[consume MP]\n0 0", []int{0}},
		{"pvp-is-not-dungeon", "[pvp]\n[consume MP]\n10 20\n[/pvp]", nil},
		{"public-fallback", "[consume MP]\n6 6\n[dungeon]\n[cool time]\n2000 2000\n[/dungeon]", []int{6}},
		{"dungeon-override", "[consume MP]\n1 1\n[maximum level]\n3\n[dungeon]\n[consume MP]\n6 10\n[/dungeon]", []int{6, 8, 10}},
		{"open-dungeon-and-pvp", "[maximum level]\n3\n[dungeon]\n[consume MP]\n6 10\n[pvp]\n[consume MP]\n100 200\n[/pvp]", []int{6, 8, 10}},
		{"negative-is-unknown", "[consume MP]\n-1 0", nil},
		{"unbounded-levels-unknown", "[consume MP]\n6 10", nil},
		{"bad-table-unknown", "[consume MP]\n6 10 20", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dungeonSkillMPCosts(tc.script); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MP = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDungeonSkillMPMatchesOriginalClient(t *testing.T) {
	// Male mage Magic Whirlwind's parsed 50-level MP vector, read from the
	// original client before any final equipment/passive cost adjustment.
	want := []int{6, 6, 7, 8, 8, 9, 10, 10, 11, 12, 12, 13, 14, 15, 15, 16, 17, 17, 18, 19,
		19, 20, 21, 21, 22, 23, 24, 24, 25, 26, 26, 27, 28, 28, 29, 30, 30, 31, 32, 33,
		33, 34, 35, 35, 36, 37, 37, 38, 39, 40}
	if got := dungeonSkillMPCosts("[maximum level]\n50\n[dungeon]\n[consume MP]\n6 40\n[/dungeon]"); !reflect.DeepEqual(got, want) {
		t.Fatalf("original client base MP differs: %v", got)
	}
}

func TestSkillStateExportKeepsBaseMPAndJobIdentity(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"sqr/example_load_state.nut": {Data: []byte(`IRDSQRCharacter.pushState(8,"example.nut","example",20,1);`)},
		"sqr/example.nut":            {Data: []byte("function onSetState_example(obj,state,data,reset) {}")},
		"skill/atmageskill.lst":      {Data: []byte("1 `atmage/example.skl`")},
		"skill/atmage/example.skl":   {Data: []byte("#PVF_File\n[maximum level]\n3\n[dungeon]\n[consume MP]\n6 10\n[cool time]\n2000 2000\n[/dungeon]")},
		"skill/swordmanskill.lst":    {Data: []byte("1 `swordman/example.skl`")},
		"skill/swordman/example.skl": {Data: []byte("#PVF_File\n[consume MP]\n100 100\n")},
	}}
	got := extractSkillStateCatalog(a)
	if len(got) != 1 || got[0].CooldownMS != 2000 || !reflect.DeepEqual(got[0].BaseMPByLevel, []int{6, 8, 10}) {
		t.Fatalf("base MP export lost the job or dungeon definition: %+v", got)
	}
}
