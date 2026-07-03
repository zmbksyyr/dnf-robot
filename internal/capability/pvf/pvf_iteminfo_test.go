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
