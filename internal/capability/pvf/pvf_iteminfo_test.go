package pvf

import (
	"robot/internal/shared"
	"strings"
	"testing"
)

func TestFormatPVFItemInfoDAT(t *testing.T) {
	text := "#PVF_File\r\n" +
		"3037\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t`無色小晶塊`\t\r\n" +
		"`name2_3037`\t\r\n" +
		"13002\t3038\t2\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t1\t`黑色大晶體`\t\r\n" +
		"`name2_3038`\t\r\n13002\t"
	got := formatPVFItemInfoDAT(text)
	lines := strings.Split(strings.TrimSpace(got), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "3037 ") || !strings.HasSuffix(lines[0], " 13002") {
		t.Fatalf("unexpected first line: %q", lines[0])
	}
	if !strings.Contains(lines[0], "`無色小晶塊` `name2_3037`") {
		t.Fatalf("first line did not keep quoted names: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "3038 ") || !strings.HasSuffix(lines[1], " 13002") {
		t.Fatalf("unexpected second line: %q", lines[1])
	}
}

func TestFormatPVFCatalogItemInfoDATGeneratesFromCatalog(t *testing.T) {
	got := formatPVFCatalogItemInfoDAT([]shared.EquipmentCatalogItem{
		{ID: 100050020, Name: "known", Level: 80, Rarity: 0, ItemType: 3, Slot: "coat", Path: "equipment/character/common/jacket/cloth/100050020.equ"},
		{ID: 3100060, Name: "halin", Level: 90, Rarity: 4, ItemType: 8, Slot: "amulet", Path: "equipment/ancient/halin/3100060.equ"},
	}, nil)
	lines := strings.Split(strings.TrimSpace(got), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "3100060 ") {
		t.Fatalf("rows not generated from catalog: %q", got)
	}
	if !strings.Contains(lines[0], "4 1 1 1 1 1 1 1 1 1 1 1 90 `halin` `name2_3100060` 11007") {
		t.Fatalf("generated row did not carry level, names, and category: %q", lines[0])
	}
}
