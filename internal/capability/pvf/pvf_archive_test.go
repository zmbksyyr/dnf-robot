package pvf

import (
	"encoding/binary"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"os"
	"path/filepath"
	"robot/internal/foundation/charset"
	"strings"
	"testing"
)

func TestOpenPVFRejectsTruncatedFileTreeEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Script.pvf")
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[16:20], 1)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := openPVF(path); err == nil || !strings.Contains(err.Error(), "entry 0 truncated") {
		t.Fatalf("openPVF error = %v, want truncated entry", err)
	}
}

func TestExtractItemListMarksPreTypeAddPropertiesIncompatible(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "add cast speed before type", body: "[add cast speed]\r\n20\r\n[equipment type]\r\n`weapon`\r\n", want: true},
		{name: "add physical attack before type", body: "[add physical attack]\r\n27\r\n[equipment type]\r\n`weapon`\r\n", want: true},
		{name: "add property after type", body: "[equipment type]\r\n`weapon`\r\n[add absolute damage]\r\n10\r\n", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := &pvfArchive{files: map[string]*pvfFile{
				"equipment/equipment.lst": {
					Name: "equipment/equipment.lst",
					Data: []byte("101030240 `character/swordman/weapon/hsword/101030240.equ`"),
				},
				"equipment/character/swordman/weapon/hsword/101030240.equ": {
					Name: "equipment/character/swordman/weapon/hsword/101030240.equ",
					Data: []byte("[name]\r\n`weapon`\r\n" + tc.body),
				},
			}}

			items := extractItemList(archive, "equipment/equipment.lst", "equipment/", false)
			if len(items) != 1 {
				t.Fatalf("items = %d, want 1", len(items))
			}
			if items[0].ClientIncompatible != tc.want {
				t.Fatalf("ClientIncompatible = %v, want %v", items[0].ClientIncompatible, tc.want)
			}
		})
	}
}

func TestScriptResolvesNStringReferences(t *testing.T) {
	archive := &pvfArchive{files: map[string]*pvfFile{
		"n_string.lst":                {Data: []byte("0 `character/character.kor.str` 14 `town/town.kor.str`")},
		"character/character.kor.str": {Data: []byte("growtype_name_0>格斗家\r\n")},
		"town/town.kor.str":           {Data: []byte("// comment\r\nname_18130>阿法利亚营地\r\n")},
	}, stringList: []string{"[name]", "name_18130", "[limit level]"}}
	data := []byte{0xb0, 0xd0}
	for _, pair := range [][2]uint32{{5, 0}, {9, 14}, {10, 1}, {5, 2}, {2, 10}} {
		data = append(data, byte(pair[0]))
		data = binary.LittleEndian.AppendUint32(data, pair[1])
	}
	text := archive.decodeScript(data)
	if !strings.Contains(text, "`阿法利亚营地`") || strings.Contains(text, "name_18130") || !strings.Contains(text, "10") {
		t.Fatalf("resolved script = %q", text)
	}
	if got := archive.nstring(0, "growtype_name_0"); got != "格斗家" {
		t.Fatalf("dictionary zero = %q", got)
	}
	if got := archive.nstring(14, "missing"); got != "ErrorString" {
		t.Fatalf("missing reference = %q", got)
	}
}

func TestStringTableUsesWholeTextEntries(t *testing.T) {
	for _, encodingName := range []string{"utf8", "gbk", "big5"} {
		t.Run(encodingName, func(t *testing.T) {
			words := []string{"[name]", "復活幣", "城鎮", "武器裝備等級"}
			if encodingName == "gbk" {
				words = []string{"[name]", "复活币", "城镇", "武器装备等级"}
			}
			var encoded [][]byte
			for _, word := range words {
				raw := []byte(word)
				if encodingName == "big5" {
					var err error
					raw, err = charset.EncodeBig5String(word)
					if err != nil {
						t.Fatal(err)
					}
				} else if encodingName == "gbk" {
					raw, _ = simplifiedchinese.GBK.NewEncoder().Bytes(raw)
				}
				encoded = append(encoded, raw)
			}
			data := make([]byte, 4+(len(words)+1)*4)
			binary.LittleEndian.PutUint32(data, uint32(len(words)))
			for i, raw := range encoded {
				binary.LittleEndian.PutUint32(data[4+i*4:], uint32(len(data)-4))
				data = append(data, raw...)
			}
			binary.LittleEndian.PutUint32(data[4+len(words)*4:], uint32(len(data)-4))
			got := decodeStringTable(data)
			if fmt.Sprint(got) != fmt.Sprint(words) {
				t.Fatalf("got %q, want %q", got, words)
			}
		})
	}
}

// Optional integration input remains outside the repository. This exercises
// the real archive/parser/catalog chain without a DB or a running server.
func TestRealPVFNames(t *testing.T) {
	path := os.Getenv("DNF_TEST_PVF")
	if path == "" {
		t.Skip("set DNF_TEST_PVF to a read-only archive")
	}
	a, err := openPVF(path)
	if err != nil {
		t.Fatal(err)
	}
	_, items, maps := extractPVFData(a)
	coin, town := "", ""
	for _, item := range items {
		if item.ID == 1 {
			coin = item.Name
		}
	}
	for _, area := range maps {
		if area.Village == 4 {
			town = area.VillageName
		}
	}
	if coin != os.Getenv("DNF_TEST_COIN") || town != os.Getenv("DNF_TEST_TOWN") {
		t.Fatalf("coin=%q town=%q", coin, town)
	}
	t.Logf("items=%d maps=%d coin=%s town=%s", len(items), len(maps), coin, town)
}
