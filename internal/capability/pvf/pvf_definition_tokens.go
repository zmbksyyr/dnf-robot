package pvf

import (
	"encoding/binary"
	"strconv"
	"strings"

	"robot/internal/foundation/charset"
)

type pvfDefinitionToken struct {
	kind     byte // PVF 2: signed integer, 5: tag, 7: literal string.
	text     string
	spelling string // Preserve native case-sensitive spellings alongside normalized text.
	value    int32
}

// Read binary types directly. Text rendering rounds floats and loses the
// distinction between tags and tag-like literals. Text fixtures use the same
// typed representation without treating floats or quoted numbers as integers.
func (archive *pvfArchive) definitionTokens(data []byte) ([]pvfDefinitionToken, bool) {
	return archive.definitionTokensWithLiteralWhitespace(data, false)
}

func (archive *pvfArchive) definitionTokensWithLiteralWhitespace(data []byte, keepWhitespace bool) ([]pvfDefinitionToken, bool) {
	if len(data) < 2 || binary.LittleEndian.Uint16(data[:2]) != 0xd0b0 {
		return textDefinitionTokensWithLiteralWhitespace(charset.DecodePVFBytes(data), keepWhitespace)
	}
	tokens := make([]pvfDefinitionToken, 0, (len(data)-2)/5)
	for i := 2; i+5 <= len(data); i += 5 {
		token := pvfDefinitionToken{kind: data[i], value: int32(binary.LittleEndian.Uint32(data[i+1 : i+5]))}
		if token.kind == 5 || token.kind == 7 {
			token.spelling = archive.lookupString(int(token.value))
			if token.kind != 7 || !keepWhitespace {
				token.spelling = strings.TrimSpace(token.spelling)
			}
			token.text = strings.ToLower(token.spelling)
		}
		tokens = append(tokens, token)
	}
	return tokens, (len(data)-2)%5 == 0
}

func textDefinitionTokens(text string) ([]pvfDefinitionToken, bool) {
	return textDefinitionTokensWithLiteralWhitespace(text, false)
}

func textDefinitionTokensWithLiteralWhitespace(text string, keepWhitespace bool) ([]pvfDefinitionToken, bool) {
	tokens := make([]pvfDefinitionToken, 0)
	text = strings.TrimPrefix(text, "\ufeff")
	for i := 0; i < len(text); {
		if isPVFSpace(text[i]) {
			i++
			continue
		}
		if text[i] == '#' || strings.HasPrefix(text[i:], "//") {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		}
		if text[i] == '`' || text[i] == '[' {
			close, kind := byte('`'), byte(7)
			if text[i] == '[' {
				close, kind = ']', 5
			}
			end := strings.IndexByte(text[i+1:], close)
			if end < 0 {
				return tokens, false
			}
			end += i + 1
			value := text[i+1 : end]
			if kind == 5 {
				value = "[" + value + "]"
			}
			if kind != 7 || !keepWhitespace {
				value = strings.TrimSpace(value)
			}
			tokens = append(tokens, pvfDefinitionToken{kind: kind, text: strings.ToLower(value), spelling: value})
			i = end + 1
			continue
		}
		start := i
		for i < len(text) && !isPVFSpace(text[i]) && text[i] != '[' && text[i] != '`' {
			i++
		}
		value, err := strconv.ParseInt(text[start:i], 10, 32)
		token := pvfDefinitionToken{text: text[start:i], spelling: text[start:i]}
		if err == nil {
			token.kind, token.value = 2, int32(value)
		}
		tokens = append(tokens, token)
	}
	return tokens, true
}
