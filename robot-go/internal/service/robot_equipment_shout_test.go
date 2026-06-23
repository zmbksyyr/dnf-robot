package service

import (
	"math/rand"
	"testing"
)

func TestEquipmentTypeRecognizesTitleAndMagicStone(t *testing.T) {
	if got := equipmentType("[title name]"); got != 2 {
		t.Fatalf("title name type got %d want 2", got)
	}
	if got := equipmentType("[magic stone]"); got != 12 {
		t.Fatalf("magic stone type got %d want 12", got)
	}
}

func TestParseJobsRecognizesMultiWordJobs(t *testing.T) {
	got := parseJobs("`[swordman]`\t\n`[at gunner]`\n`[thief]`\n`[at fighter]`\n`[at mage]`\n`[demonic swordman]`")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})

	got = parseJobs("swordman at gunner thief at fighter at mage demonic swordman")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})
}

func TestPrepareShoutSeparatesLocalAndWorld(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")

	localType, localChannel, localOut := m.prepareShout(shoutTemplates{}, "hello", false)
	if localType != 3 || localChannel != "local" || localOut != "hello" {
		t.Fatalf("local shout got type=%d channel=%s out=%q", localType, localChannel, localOut)
	}

	worldType, worldChannel, worldOut := m.prepareShout(shoutTemplates{}, "hello", true)
	if worldType != 11 || worldChannel != "world" || worldOut != "hello" {
		t.Fatalf("world shout got type=%d channel=%s out=%q, want type=11 channel=world out=hello", worldType, worldChannel, worldOut)
	}
}

func TestWriteEquipSlotUsesHighIntensify(t *testing.T) {
	rc := robotRuntimeConfig{EquipIntensifyMin: 0, EquipIntensifyMax: 10}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		writeEquipSlot(raw, equipmentCatalogItem{ID: 1000 + i, ItemType: 3}, rng, rc)
		if raw[6] < 7 || raw[6] > 10 {
			t.Fatalf("armor intensify got %d want 7..10", raw[6])
		}
	}
	for i := 0; i < 100; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		raw := make([]byte, 61)
		writeEquipSlot(raw, equipmentCatalogItem{ID: 2000 + i, ItemType: 1}, rng, rc)
		if raw[6] < 8 || raw[6] > 15 {
			t.Fatalf("weapon intensify got %d want 8..15", raw[6])
		}
	}
}
