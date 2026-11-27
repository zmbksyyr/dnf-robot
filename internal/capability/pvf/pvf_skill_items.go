package pvf

import (
	"strings"

	"robot/internal/shared"
)

// The native material getter (0x912760) only applies modifiers while walking
// existing consume-item rows. An empty row vector stays empty. Until runtime
// inventory checks and consumption are available, only this case can bypass
// them. Check every mode so this proof does not depend on guessing actualmode.
func (archive *pvfArchive) skillItemCostFree(path string) bool {
	file := archive.files[path]
	if file == nil || len(file.Data) == 0 {
		return false
	}
	tokens, valid := archive.definitionTokens(file.Data)
	return skillItemCostFreeFromTokens(tokens, valid)
}

func skillItemCostFreeFromTokens(tokens []pvfDefinitionToken, valid bool) bool {
	if !valid || len(tokens) == 0 {
		return false
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.kind != 5 {
			continue
		}
		if token.spelling == "" {
			return false
		}
		switch token.text {
		case "[consume item]":
			// A row whose current count is zero still needs the real item
			// modifier pipeline. Only an explicitly empty block is free.
			if token.spelling != token.text || i+1 >= len(tokens) ||
				tokens[i+1].kind != 5 || tokens[i+1].spelling != "[/consume item]" {
				return false
			}
			i++
		case "[/consume item]":
			return false
		}
	}
	return true
}

func skillMaterialDefinitionFromTokens(tokens []pvfDefinitionToken, valid bool) shared.SkillMaterialDefinition {
	unknown := shared.SkillMaterialDefinition{}
	if !valid || len(tokens) == 0 {
		return unknown
	}
	definition := shared.SkillMaterialDefinition{Known: true}
	paired := map[string]bool{"[dungeon]": true, "[pvp]": true, "[death tower]": true, "[warroom]": true,
		"[if]": true, "[then]": true, "[multiple then]": true, "[module]": true}
	for _, token := range tokens {
		if token.kind == 5 && strings.HasPrefix(token.text, "[/") {
			paired["["+token.text[2:]] = true
		}
	}
	var scopes []string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.kind != 5 {
			continue
		}
		if token.spelling == "" || (paired[token.text] || strings.HasPrefix(token.text, "[/") ||
			token.text == "[maximum level]") && token.spelling != token.text {
			return unknown
		}
		switch token.text {
		case "[maximum level]":
			if len(scopes) != 0 || definition.MaximumLevel != 0 || i+1 >= len(tokens) ||
				tokens[i+1].kind != 2 || tokens[i+1].value <= 0 ||
				i+2 < len(tokens) && tokens[i+2].kind != 5 {
				return unknown
			}
			definition.MaximumLevel = tokens[i+1].value
			i++
		case "[consume item]":
			mode := "common"
			if len(scopes) == 1 {
				switch scopes[0] {
				case "[dungeon]", "[pvp]", "[death tower]", "[warroom]":
					mode = strings.Trim(scopes[0], "[]")
				default:
					return unknown
				}
			} else if len(scopes) != 0 {
				return unknown
			}
			end := i + 1
			for end < len(tokens) && tokens[end].kind == 2 {
				end++
			}
			if end >= len(tokens) || tokens[end].kind != 5 || tokens[end].spelling != "[/consume item]" ||
				(end-i-1)%3 != 0 {
				return unknown
			}
			block := shared.SkillMaterialBlock{Mode: mode, Rows: make([]shared.SkillMaterialRow, 0, (end-i-1)/3)}
			for at := i + 1; at < end; at += 3 {
				// Zero IDs and signed endpoints are retained as native data;
				// only a resolved consumption request may decide what can be spent.
				block.Rows = append(block.Rows, shared.SkillMaterialRow{ItemID: tokens[at].value,
					Start: tokens[at+1].value, End: tokens[at+2].value})
			}
			definition.Blocks = append(definition.Blocks, block)
			i = end
		case "[/consume item]":
			return unknown
		default:
			if strings.HasPrefix(token.text, "[/") {
				if len(scopes) == 0 || scopes[len(scopes)-1] != "["+token.text[2:] {
					return unknown
				}
				scopes = scopes[:len(scopes)-1]
			} else if paired[token.text] {
				scopes = append(scopes, token.text)
			}
		}
	}
	if len(scopes) != 0 || definition.MaximumLevel == 0 {
		return unknown
	}
	return definition
}
