package pvf

import (
	"reflect"
	"testing"
)

func TestPVFListQuotedWhitespacePreservesFollowingEntries(t *testing.T) {
	text := "#PVF_File\n1 `character/with space.equ`\n2 `character/ordinary.equ`\n" +
		"// a comment is not an entry\n3 plain.equ\n4 `character/another space.equ`\n5 `LAST.EQU`\n"
	want := []pvfListEntry{
		{ID: 1, Path: "character/with space.equ"}, {ID: 2, Path: "character/ordinary.equ"},
		{ID: 3, Path: "plain.equ"}, {ID: 4, Path: "character/another space.equ"}, {ID: 5, Path: "last.equ"},
	}
	if got := parsePVFList(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("quoted paths shifted list pairs: %+v want %+v", got, want)
	}
	archive := &pvfArchive{files: map[string]*pvfFile{"example.lst": {Data: []byte(text)}}}
	if got := parsePVFList(archive.text("example.lst")); !reflect.DeepEqual(got, want) {
		t.Fatalf("plain archive text lost the final quoted path: %+v", got)
	}
	archive.files["example.equ"] = &pvfFile{Data: []byte("[pvp]\n[/pvp]")}
	if got := archive.text("example.equ"); got != "[pvp]\n[/pvp]" {
		t.Fatalf("plain archive text lost structural brackets: %q", got)
	}
	if got := parsePVFList("1 `unterminated"); len(got) != 0 {
		t.Fatalf("unterminated list literal accepted: %+v", got)
	}
}
