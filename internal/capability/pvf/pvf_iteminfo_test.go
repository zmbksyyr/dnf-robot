package pvf

import (
	"robot/internal/shared"
	"strings"
	"testing"
)

func TestFormatPVFItemInfoDAT(t *testing.T) {
	text := "#PVF_File\r\n" +
		"3037\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t`item_3037`\t\r\n" +
		"`name2_3037`\t\r\n" +
		"13002\t3038\t2\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t`item_3038`\t\r\n" +
		"`name2_3038`\t\r\n13002\t"
	got := formatPVFItemInfoDAT(text)
	lines := strings.Split(strings.TrimSpace(got), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "3037 ") || !strings.HasSuffix(lines[0], " 13002") {
		t.Fatalf("unexpected first line: %q", lines[0])
	}
	if !strings.Contains(lines[0], "`item_3037` `name2_3037`") {
		t.Fatalf("first line did not keep quoted names: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "3038 ") || !strings.HasSuffix(lines[1], " 13002") {
		t.Fatalf("unexpected second line: %q", lines[1])
	}
}

func TestFormatExtendedPVFItemInfoDATKeepsRawAndGeneratesPVFItems(t *testing.T) {
	raw := "2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `gold` `gold` 13002\r\n" +
		"3100060 0 1 1 1 1 1 1 1 1 1 1 1 90 `raw` `raw2` 99999\r\n"
	got := formatExtendedPVFItemInfoDAT(raw, []shared.EquipmentCatalogItem{
		{ID: 3100060, Level: 90, Rarity: 4, ItemType: 8, Slot: "amulet", Path: "equipment/ancient/halin/3100060.equ"},
		{ID: 35500001, Level: 90, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 3, Path: "equipment/character/fighter/weapon/boxglove/35500001.equ", UseJob: []int{1, 7}},
		{ID: 28237, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 3, Path: "equipment/character/swordman/weapon/beamsword/28237.equ"},
		{ID: 37603, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/wand/37603.equ"},
		{ID: 37604, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/twinsword/37604.equ"},
		{ID: 37605, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/dagger/37605.equ"},
		{ID: 100050203, Level: 85, Rarity: 4, ItemType: 3, Slot: "coat", Path: "equipment/character/common/jacket/cloth/100050203.equ"},
	}, []shared.EquipmentCatalogItem{
		{ID: 5057, Level: 85, Rarity: 1, Slot: "recipe", Path: "stackable/recipe/rcp_cloth_piece2.stk"},
	})
	lines := strings.Split(strings.TrimSpace(got), "\r\n")
	if len(lines) != 9 {
		t.Fatalf("lines = %d, want 9: %q", len(lines), got)
	}
	assertLineContains(t, lines, "2675336 ", "13002")
	assertLineContains(t, lines, "3100060 ", "12001")
	assertLineHasToken(t, lines, "3100060 ", 13, "70")
	if strings.Contains(got, "99999") || strings.Contains(got, "`raw`") {
		t.Fatalf("raw iteminfo row was not overwritten by PVF generated row: %q", got)
	}
	assertLineContains(t, lines, "35500001 ", "10205")
	assertLineHasToken(t, lines, "35500001 ", 2, "1")
	assertLineHasToken(t, lines, "35500001 ", 3, "1")
	assertLineHasToken(t, lines, "35500001 ", 12, "1")
	assertLineHasToken(t, lines, "35500001 ", 13, "70")
	assertLineContains(t, lines, "28237 ", "10106")
	assertLineContains(t, lines, "37603 ", "10604")
	assertLineContains(t, lines, "37604 ", "10603")
	assertLineContains(t, lines, "37605 ", "10602")
	assertLineContains(t, lines, "100050203 ", "11002")
	assertLineHasToken(t, lines, "100050203 ", 13, "70")
	assertLineContains(t, lines, "5057 ", "31305")
	assertLineHasToken(t, lines, "5057 ", 13, "70")
}

func assertLineContains(t *testing.T, lines []string, prefix, want string) {
	t.Helper()
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			if !strings.HasSuffix(line, " "+want) {
				t.Fatalf("line %q does not end with category %s", line, want)
			}
			return
		}
	}
	t.Fatalf("missing line prefix %q in %#v", prefix, lines)
}

func assertLineHasToken(t *testing.T, lines []string, prefix string, tokenIndex int, want string) {
	t.Helper()
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			fields := strings.Fields(line)
			if tokenIndex < 0 || tokenIndex >= len(fields) {
				t.Fatalf("line %q has %d fields, missing token %d", line, len(fields), tokenIndex)
			}
			if fields[tokenIndex] != want {
				t.Fatalf("line %q token %d = %q, want %q", line, tokenIndex, fields[tokenIndex], want)
			}
			return
		}
	}
	t.Fatalf("missing line prefix %q in %#v", prefix, lines)
}
