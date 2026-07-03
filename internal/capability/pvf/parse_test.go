package pvf

import "testing"

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
}

func TestParseJobsRecognizesMultiWordJobs(t *testing.T) {
	got := parseJobs("`[swordman]`\t\n`[at gunner]`\n`[thief]`\n`[at fighter]`\n`[at mage]`\n`[demonic swordman]`")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})

	got = parseJobs("swordman at gunner thief at fighter at mage demonic swordman")
	assertIntSlice(t, got, []int{0, 5, 6, 7, 8, 9})
}
