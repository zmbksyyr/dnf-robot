package charset

import "testing"

func TestDecodeGameTextPreservesUnicodeAndWesternText(t *testing.T) {
	for _, text := range []string{"", "ascii", "中文名字12", "Café", "Ã", "emoji😀", "中文Café"} {
		if got := DecodeGameText(text); got != text {
			t.Errorf("DecodeGameText(%q) = %q", text, got)
		}
	}
}

func TestDecodeGameTextRestoresOriginalMySQLNameBytes(t *testing.T) {
	for _, name := range []string{"改名联调甲", "六字中文名字12", "Café中文", "a'\\b"} {
		encoded := UTF8AsWindows1252String(name)
		if got := DecodeGameText(encoded); got != name {
			t.Errorf("decoded MySQL latin1 value %q = %q, want %q", encoded, got, name)
		}
		var latin1 []rune
		for _, b := range []byte(name) {
			latin1 = append(latin1, rune(b))
		}
		if got := DecodeGameText(string(latin1)); got != name {
			t.Errorf("decoded raw Latin1 runes = %q, want %q", got, name)
		}
	}
}
