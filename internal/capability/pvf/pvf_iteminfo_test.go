package pvf

import (
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

func TestParsePVFItemInfoCatalog(t *testing.T) {
	text := "2675336 2 1 1 1 1 1 1 1 1 1 1 1 1 `100萬金幣` `100萬金幣` 13002\r\n" +
		"31056 2 0 0 1 0 0 1 0 0 0 0 0 25 `weapon` `name2_31056` 10302\r\n"
	got := parsePVFItemInfoCatalog(text)
	if len(got) != 2 {
		t.Fatalf("items = %d, want 2: %#v", len(got), got)
	}
	if got[0].ID != 31056 || got[0].Category != 10302 || got[0].Level != 25 || len(got[0].UseJob) != 2 {
		t.Fatalf("unexpected sorted equipment item: %#v", got[0])
	}
	if got[1].ID != 2675336 || got[1].Category != 13002 || got[1].Name != "100萬金幣" {
		t.Fatalf("unexpected gold item: %#v", got[1])
	}
}
