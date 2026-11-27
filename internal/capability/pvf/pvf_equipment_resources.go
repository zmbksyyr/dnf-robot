package pvf

import (
	"strconv"
	"strings"

	"robot/internal/shared"
)

func equipmentResources(body string) *shared.EquipmentResources {
	tokens, _ := textDefinitionTokens(body)
	paired := make(map[string]bool)
	for _, token := range tokens {
		if token.kind == 5 && strings.HasPrefix(token.text, "[/") {
			paired["["+token.text[2:]] = true
		}
	}
	out := &shared.EquipmentResources{}
	var base shared.ResourceStats
	var stack []string
	setIndex := -1
	for i, token := range tokens {
		if token.kind == 5 && strings.HasPrefix(token.text, "[/") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if token.text == "[/piece set ability]" || token.text == "[/set ability]" {
				setIndex = -1
			}
			continue
		}
		if token.kind == 5 && paired[token.text] {
			if len(stack) == 0 && (token.text == "[piece set ability]" || token.text == "[set ability]") {
				items := pvfSetItemIDs(body)
				pieces := len(items)
				if token.text == "[piece set ability]" && i+1 < len(tokens) {
					pieces = int(tokens[i+1].value)
				}
				out.Sets = append(out.Sets, shared.EquipmentResourceSet{Items: items, Pieces: pieces})
				setIndex = len(out.Sets) - 1
			}
			stack = append(stack, token.text)
			continue
		}
		// Avatar options are one-based in USERINFO's hidden-option DWORD. A job
		// literal immediately following SKILL_LEVEL belongs to that option.
		if len(stack) == 1 && stack[0] == "[avatar select ability]" && token.kind == 7 && strings.HasPrefix(token.text, "[") {
			if i > 0 && tokens[i-1].text == "[skill_level]" {
				continue
			}
			out.Options = append(out.Options, shared.ResourceStats{})
			if i+2 < len(tokens) {
				resourceStat(&out.Options[len(out.Options)-1], token.text, tokens[i+2])
			}
			continue
		}
		if token.kind != 5 || i+1 >= len(tokens) {
			continue
		}
		if len(stack) == 0 {
			resourceStat(&base, token.text, tokens[i+1])
		} else if len(stack) == 1 && setIndex >= 0 {
			var stat shared.ResourceStats
			resourceStat(&stat, token.text, tokens[i+1])
			set := &out.Sets[setIndex]
			set.HP += stat.HP
			set.MP += stat.MP
			set.Vitality += stat.Vitality
			set.Spirit += stat.Spirit
		}
	}
	out.HP, out.MP, out.Vitality, out.Spirit = base.HP, base.MP, base.Vitality, base.Spirit
	out.HPRate, out.MPRate = base.HPRate, base.MPRate
	return out
}

func resourceStat(out *shared.ResourceStats, tag string, token pvfDefinitionToken) {
	value := int(token.value)
	switch tag {
	case "[hp max]":
		out.HP += value
	case "[mp max]":
		out.MP += value
	case "[physical defense]":
		out.Vitality += value
	case "[magical defense]":
		out.Spirit += value
	case "[hp max rate]", "[mp max rate]":
		rate, _ := strconv.ParseFloat(token.text, 32)
		if token.kind == 2 {
			rate = float64(token.value)
		}
		if tag == "[hp max rate]" {
			out.HPRate += float32(rate) / 100
		} else {
			out.MPRate += float32(rate) / 100
		}
	}
}
