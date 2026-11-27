package dnf

import "testing"

// The live server sends an entry for a skill slot that carries no level. That
// must not discard the whole tree: one empty slot used to disable casting for
// the entire session.
func TestParsePartyLearnedSkillsSkipsEmptyEntries(t *testing.T) {
	body := []byte{0x00, 0x00, 0x03}
	body = append(body, 0x00, 4, 1, 0)              // slot 0: skill 4 at level 1
	body = append(body, 0x01, 0xeb, 0, 0)           // slot 1: skill 235 with no level
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

func TestParsePartyLearnedSkillsCombinesExplicitAndAutomaticSlots(t *testing.T) {
	// Newly created swordman: the server lists learned craft/disjoint skills
	// and appends the same common skills in its automatic slots.
	body := []byte{0xa9, 0x5b, 5,
		24, 46, 7, 1, 4,
		69, 179, 10, 0,
		73, 184, 2, 0,
		105, 179, 7, 0,
		104, 184, 1, 0,
	}
	levels, ok := parsePartyLearnedSkills(body)
	if !ok || levels[46] != 7 || levels[179] != 10 || levels[184] != 2 {
		t.Fatalf("server-normalized skills were discarded: valid=%t upper=%d disjoint=%d craft=%d", ok, levels[46], levels[179], levels[184])
	}
}
