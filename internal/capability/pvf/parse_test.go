package pvf

import (
	"testing"

	"robot/internal/shared"
)

func assertIntSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length got %d want %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] got %d want %d: got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func TestEquipmentTypeRecognizesTitleAndMagicStone(t *testing.T) {
	if got := equipmentType("[title name]"); got != 2 {
		t.Fatalf("title name type got %d want 2", got)
	}
	if got := equipmentType("[magic stone]"); got != 12 {
		t.Fatalf("magic stone type got %d want 12", got)
	}
	if got := equipmentType("[red artifact]"); got != 31 {
		t.Fatalf("red artifact type got %d want 31", got)
	}
	if got := equipmentType("[creature blue artifact]"); got != 32 {
		t.Fatalf("creature blue artifact type got %d want 32", got)
	}
	if got := equipmentType("[pet_green_artifact]"); got != 33 {
		t.Fatalf("pet green artifact type got %d want 33", got)
	}
}

func TestParseJobsRecognizesMultiWordJobs(t *testing.T) {
	got := parseJobs("`[swordman]`\t\n`[at gunner]`\n`[thief]`\n`[at fighter]`\n`[at mage]`\n`[at priest]`\n`[demonic swordman]`")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 14, 9})

	got = parseJobs("swordman at gunner thief at fighter at mage at priest demonic swordman")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 14, 9})
}

func TestPriestAvatarPathKeepsGenderJob(t *testing.T) {
	if got := jobFromEquipmentPath("character/priest/avatar/coat/100.equ"); got != 4 {
		t.Fatalf("male priest avatar job got %d want 4", got)
	}
	if got := jobFromEquipmentPath("character/priest/at_avatar/coat/200.equ"); got != 14 {
		t.Fatalf("female priest avatar job got %d want 14", got)
	}
}

func TestEquipmentExplicitJobsOverridePathFallback(t *testing.T) {
	item := shared.EquipmentCatalogItem{ItemType: 1, UseJob: []int{14}}
	applyEquipmentPathJob(&item, 4)
	assertIntSlice(t, item.UseJob, []int{14})

	item = shared.EquipmentCatalogItem{ItemType: 1}
	applyEquipmentPathJob(&item, 4)
	assertIntSlice(t, item.UseJob, []int{4})

	item = shared.EquipmentCatalogItem{ItemType: 23, UseJob: []int{4}}
	applyEquipmentPathJob(&item, 14)
	assertIntSlice(t, item.UseJob, []int{14})
}

func TestAppendItemInfoCreatureArtifacts(t *testing.T) {
	raw := "#PVF_File\r\n" +
		"63500 1 1 1 1 1 1 1 1 1 1 1 1 70 `red` `red2` 14002\r\n" +
		"64000 2 1 1 1 1 1 1 1 1 1 1 1 70 `blue` `blue2` 14003\r\n" +
		"64500 3 1 1 1 1 1 1 1 1 1 1 1 70 `green` `green2` 14004\r\n" +
		"63000 1 1 1 1 1 1 1 1 1 1 1 1 70 `creature` `creature2` 14001\r\n"
	got := appendItemInfoCreatureArtifacts(nil, raw)
	if len(got) != 3 {
		t.Fatalf("artifact count got %d want 3: %#v", len(got), got)
	}
	if got[0].ID != 63500 || got[0].Slot != "artifact red" || got[0].ItemType != 31 {
		t.Fatalf("red artifact not parsed: %#v", got[0])
	}
	if got[1].ID != 64000 || got[1].Slot != "artifact blue" || got[1].ItemType != 32 {
		t.Fatalf("blue artifact not parsed: %#v", got[1])
	}
	if got[2].ID != 64500 || got[2].Slot != "artifact green" || got[2].ItemType != 33 {
		t.Fatalf("green artifact not parsed: %#v", got[2])
	}
}
