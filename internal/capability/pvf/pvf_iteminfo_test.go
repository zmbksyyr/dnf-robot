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
	raw := "2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `百萬金幣` `金币` 13002\r\n" +
		"3100060 0 1 1 1 1 1 1 1 1 1 1 1 90 `raw` `raw2` 99999\r\n" +
		"101030240 0 1 1 1 1 1 1 1 1 1 1 1 75 `bad` `bad2` 10103\r\n" +
		"2600471 2 1 1 1 1 1 1 1 1 1 1 1 1 `doll` `doll2` 33002\r\n" +
		"2610030 1 1 1 1 1 1 1 1 1 1 1 1 1 `material` `material2` 33001\r\n"
	got := formatExtendedPVFItemInfoDAT(raw, []shared.EquipmentCatalogItem{
		{ID: 101030240, ItemType: 1, Slot: "weapon", ClientIncompatible: true},
		{ID: 3100060, Name: "無法編碼的名稱", Name2: "无法编码的名称", Level: 90, Rarity: 4, ItemType: 8, Slot: "amulet", Path: "equipment/ancient/halin/3100060.equ"},
		{ID: 35500001, Level: 90, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 3, Path: "equipment/character/fighter/weapon/boxglove/35500001.equ", UseJob: []int{1, 7}},
		{ID: 28237, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 3, Path: "equipment/character/swordman/weapon/beamsword/28237.equ"},
		{ID: 37603, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/wand/37603.equ"},
		{ID: 37604, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/twinsword/37604.equ"},
		{ID: 37605, Level: 85, Rarity: 4, ItemType: 1, Slot: "weapon", SubType: 1, Path: "equipment/character/thief/weapon/dagger/37605.equ"},
		{ID: 100050203, Level: 85, Rarity: 4, ItemType: 3, Slot: "coat", Path: "equipment/character/common/jacket/cloth/100050203.equ"},
	}, []shared.EquipmentCatalogItem{
		{ID: 5057, Level: 85, Rarity: 1, Slot: "recipe", Path: "stackable/recipe/rcp_cloth_piece2.stk"},
		{ID: 2600471, Level: 1, Rarity: 2, Slot: "expert town potion", Path: "stackable/professional/common/doll_shop1.stk"},
		{ID: 2610030, Level: 1, Rarity: 1, Slot: "material expert job", Path: "stackable/professional/material/crystallization_magical.stk"},
		{ID: 2700001, Level: 1, Rarity: 2, Slot: "waste", Path: "stackable/professional/potion/new_potion.stk"},
		{ID: 2700002, Level: 1, Rarity: 2, Slot: "expert town potion", Path: "stackable/professional/puppet/new_puppet.stk"},
	})
	lines := strings.Split(strings.TrimSpace(got), "\r\n")
	if len(lines) != 13 {
		t.Fatalf("lines = %d, want 13: %q", len(lines), got)
	}
	for _, b := range []byte(got) {
		if b >= 0x80 {
			t.Fatalf("extended iteminfo contains non-ASCII byte 0x%x: %q", b, got)
		}
	}
	assertLineContains(t, lines, "2675336 ", "13002")
	assertLineHasToken(t, lines, "2675336 ", 14, "`item_2675336`")
	assertLineHasToken(t, lines, "2675336 ", 15, "`name2_2675336`")
	assertLineContains(t, lines, "3100060 ", "12001")
	assertLineHasToken(t, lines, "3100060 ", 14, "`item_3100060`")
	assertLineHasToken(t, lines, "3100060 ", 15, "`name2_3100060`")
	assertLineHasToken(t, lines, "3100060 ", 13, "70")
	if strings.Contains(got, "99999") || strings.Contains(got, "`raw`") {
		t.Fatalf("raw iteminfo row was not overwritten by PVF generated row: %q", got)
	}
	if strings.Contains(got, "101030240") {
		t.Fatalf("client-incompatible equipment survived iteminfo filtering: %q", got)
	}
	assertLineContains(t, lines, "35500001 ", "10205")
	assertLineHasToken(t, lines, "35500001 ", 2, "0")
	assertLineHasToken(t, lines, "35500001 ", 3, "1")
	assertLineHasToken(t, lines, "35500001 ", 9, "1")
	assertLineHasToken(t, lines, "35500001 ", 12, "0")
	assertLineHasToken(t, lines, "35500001 ", 13, "70")
	assertLineContains(t, lines, "28237 ", "10106")
	assertLineContains(t, lines, "37603 ", "10604")
	assertLineContains(t, lines, "37604 ", "10603")
	assertLineContains(t, lines, "37605 ", "10602")
	assertLineContains(t, lines, "100050203 ", "11002")
	assertLineHasToken(t, lines, "100050203 ", 13, "70")
	assertLineContains(t, lines, "5057 ", "31305")
	assertLineHasToken(t, lines, "5057 ", 13, "70")
	assertLineContains(t, lines, "2600471 ", "33002")
	assertLineContains(t, lines, "2610030 ", "33001")
	assertLineContains(t, lines, "2700001 ", "33001")
	assertLineContains(t, lines, "2700002 ", "33002")
}

func TestApplyRawItemInfoSearchFieldsPreservesFineCategoryAndJobs(t *testing.T) {
	generated := []string{"1", "2", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "70", "`item`", "`name`", "32100"}
	raw := []string{"1", "2", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "70", "`raw`", "`raw2`", "32105"}
	if !applyRawItemInfoSearchFields(generated, raw) {
		t.Fatal("valid raw search fields were not preserved")
	}
	if generated[3] != "1" || generated[2] != "0" || generated[4] != "0" || generated[16] != "32105" {
		t.Fatalf("preserved search fields = %v", generated)
	}
}

func TestGeneratedItemInfoJobFlagsUsePVFJobs(t *testing.T) {
	fields := generatedItemInfoJobFlags(shared.EquipmentCatalogItem{UseJob: []int{2, 6}}, false)
	for index, value := range fields {
		want := "0"
		if index == 2 || index == 6 {
			want = "1"
		}
		if value != want {
			t.Fatalf("job flag %d = %q, want %q: %v", index, value, want, fields)
		}
	}
}

func TestGeneratedRecipeItemInfoCategoryUsesTargetEquipment(t *testing.T) {
	equipment := map[int]shared.EquipmentCatalogItem{
		100: {ID: 100, Slot: "weapon", Path: "equipment/character/fighter/weapon/boxglove/item.equ"},
		101: {ID: 101, Slot: "coat", Path: "equipment/character/common/jacket/leather/item.equ"},
		102: {ID: 102, Slot: "support", Path: "equipment/character/common/item.equ"},
		103: {ID: 103, Slot: "magic stone", Path: "equipment/character/common/item.equ"},
	}
	for _, tt := range []struct {
		target int
		want   int
	}{{100, 31003}, {101, 31103}, {102, 31302}, {103, 31303}} {
		item := shared.EquipmentCatalogItem{Slot: "recipe", RecipeTargetID: tt.target}
		if got := generatedRecipeItemInfoCategory(item, equipment); got != tt.want {
			t.Fatalf("recipe target %d category = %d, want %d", tt.target, got, tt.want)
		}
	}
}

func TestGeneratedStackableFineCategories(t *testing.T) {
	for _, tt := range []struct {
		item shared.EquipmentCatalogItem
		want int
	}{
		{shared.EquipmentCatalogItem{Slot: "throw", Path: "stackable/throw/new.stk"}, 13003},
		{shared.EquipmentCatalogItem{Slot: "set", Path: "stackable/throw/trap.stk"}, 13003},
		{shared.EquipmentCatalogItem{Slot: "quest receive", Path: "stackable/quest/new.stk"}, 13005},
	} {
		if got := generatedStackableItemInfoCategory(tt.item); got != tt.want {
			t.Fatalf("item %#v category = %d, want %d", tt.item, got, tt.want)
		}
	}
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
