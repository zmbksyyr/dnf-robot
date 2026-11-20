package dnf

import "testing"

// The live server sends an entry for a skill slot that carries no level. That
// must not discard the whole tree: one empty slot used to disable casting for
// the entire session.
func TestParsePartyLearnedSkillsSkipsEmptyEntries(t *testing.T) {
	body := []byte{0x00, 0x00, 0x03}
	body = append(body, 0x00, 4, 1, 0)          // slot 0: skill 4 at level 1
	body = append(body, 0x01, 0xeb, 0, 0)        // slot 1: skill 235 with no level
	body = append(body, 0x02, 47, 3, 2, 0xaa, 0xbb) // slot 2: skill 47 at level 3

	levels, ok := parsePartyLearnedSkills(body)
	if !ok {
		t.Fatal("a zero-level entry discarded the whole skill tree")
	}
	if levels[4] != 1 || levels[47] != 3 {
		t.Fatalf("levels = %d %d", levels[4], levels[47])
	}
	if levels[0xeb] != 0 {
		t.Fatalf("unlearned skill recorded at level %d", levels[0xeb])
	}
}

func TestParsePartyLearnedSkillsRejectsTruncatedEntry(t *testing.T) {
	body := []byte{0x00, 0x00, 0x02, 0x00, 4, 1, 0}
	if _, ok := parsePartyLearnedSkills(body); ok {
		t.Fatal("truncated entry was accepted")
	}
}

func TestParsePartyLearnedSkillsRejectsDuplicateSlot(t *testing.T) {
	body := []byte{0x00, 0x00, 0x02, 0x00, 4, 1, 0, 0x00, 5, 1, 0}
	if _, ok := parsePartyLearnedSkills(body); ok {
		t.Fatal("duplicate slot was accepted")
	}
}
