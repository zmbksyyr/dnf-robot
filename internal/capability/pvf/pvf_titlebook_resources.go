package pvf

import "robot/internal/shared"

// Repeated collection/section blocks are keyed by the native category names.
// Reject incomplete or unfamiliar definitions instead of publishing a subset
// that could later turn an unknown chapter into a completed one.
func parseTitlebookCollectionResources(tokens []pvfDefinitionToken) ([]shared.TitlebookCategoryResources, bool) {
	collections, valid := titlebookDefinitionBlocks(tokens, "title collection info")
	if !valid || len(collections) != 4 {
		return nil, false
	}
	sections, valid := titlebookDefinitionBlocks(tokens, "section")
	if !valid || len(sections) != 4 {
		return nil, false
	}
	out := make([]shared.TitlebookCategoryResources, 4)
	seen := [4]bool{}
	for _, block := range collections {
		category, valid := titlebookDefinitionCategory(block)
		if !valid || seen[category] || len(block) < 2 || block[1].kind != 2 || block[1].value <= 0 || block[1].value > 200 {
			return nil, false
		}
		seen[category] = true
		definition := &out[category]
		definition.Category = category
		definition.Elements = make([]shared.TitlebookElementResources, int(block[1].value))
		cursor := 2
		for slot := range definition.Elements {
			if cursor+2 > len(block) || block[cursor].kind != 2 || block[cursor].value != int32(slot) || block[cursor+1].kind != 2 {
				return nil, false
			}
			element := shared.TitlebookElementResources{Type: block[cursor+1].value, Achievement: -1}
			cursor += 2
			if element.Type < -1 || element.Type > 1 {
				return nil, false
			}
			if element.Type != -1 {
				if cursor+2 > len(block) || block[cursor].kind != 2 || block[cursor].value < -1 || block[cursor+1].kind != 2 {
					return nil, false
				}
				element.Achievement = block[cursor].value
				count := int(block[cursor+1].value)
				cursor += 2
				if count < 0 || count > 200 || count > len(block)-cursor {
					return nil, false
				}
				for _, title := range block[cursor : cursor+count] {
					if title.kind != 2 || title.value < -1 {
						return nil, false
					}
					element.Titles = append(element.Titles, title.value)
				}
				cursor += count
			}
			definition.Elements[slot] = element
		}
		if cursor != len(block) {
			return nil, false
		}
	}
	seen = [4]bool{}
	for _, block := range sections {
		category, valid := titlebookDefinitionCategory(block)
		if !valid || seen[category] {
			return nil, false
		}
		seen[category] = true
		definition := &out[category]
		parsed, valid := parseTitlebookResourceSections(block[1:], definition.Elements)
		if !valid {
			return nil, false
		}
		definition.Sections = parsed
	}
	return out, true
}

func titlebookDefinitionCategory(block []pvfDefinitionToken) (int, bool) {
	if len(block) == 0 || block[0].kind != 7 {
		return 0, false
	}
	for category, name := range []string{"general", "specific", "despair", "event"} {
		if block[0].text == name {
			return category, true
		}
	}
	return 0, false
}

func titlebookDefinitionBlocks(tokens []pvfDefinitionToken, name string) ([][]pvfDefinitionToken, bool) {
	var out [][]pvfDefinitionToken
	start := -1
	for i, token := range tokens {
		if token.kind != 5 {
			continue
		}
		switch token.text {
		case "[" + name + "]":
			if start >= 0 {
				return nil, false
			}
			start = i + 1
		case "[/" + name + "]":
			if start < 0 {
				return nil, false
			}
			out = append(out, tokens[start:i])
			start = -1
		}
	}
	return out, start < 0
}

func parseTitlebookResourceSections(tokens []pvfDefinitionToken, elements []shared.TitlebookElementResources) ([]shared.TitlebookSectionResources, bool) {
	var out []shared.TitlebookSectionResources
	for len(tokens) > 0 {
		if tokens[0].kind != 5 {
			return nil, false
		}
		if tokens[0].text == "[section icon]" {
			if len(tokens) < 3 || tokens[1].kind != 7 || tokens[2].kind != 2 {
				return nil, false
			}
			tokens = tokens[3:]
			continue
		}
		if len(tokens) < 4 || tokens[0].text != "[section name]" || tokens[1].kind != 7 {
			return nil, false
		}
		tokens = tokens[2:]
		var frameType int32
		seen := map[string]bool{}
		for len(tokens) > 0 && tokens[0].kind == 5 && (tokens[0].text == "[frame type]" || tokens[0].text == "[lock message]") {
			tag := tokens[0].text
			kind := byte(7)
			if tag == "[frame type]" {
				kind = 2
			}
			if len(tokens) < 2 || tokens[1].kind != kind || seen[tag] {
				return nil, false
			}
			seen[tag] = true
			if tag == "[frame type]" {
				frameType = tokens[1].value
			}
			tokens = tokens[2:]
		}
		if len(tokens) < 2 || tokens[0].kind != 5 || tokens[0].text != "[reward type]" {
			return nil, false
		}
		close := 1
		for close < len(tokens) && (tokens[close].kind != 5 || tokens[close].text != "[/reward type]") {
			close++
		}
		if close == len(tokens) {
			return nil, false
		}
		effects, valid := parseTitlebookSectionEffects(tokens[1:close])
		if !valid {
			return nil, false
		}
		start, end := len(out)*10, (len(out)+1)*10
		if end > len(elements) {
			return nil, false
		}
		// Native importTitleSection starts at ordinal*10. Both parsers then
		// truncate at the first vacant element, even if later slots are filled.
		for slot := start; slot < end; slot++ {
			if elements[slot].Type == -1 {
				end = slot
				break
			}
		}
		out = append(out, shared.TitlebookSectionResources{Start: start, End: end, FrameType: frameType, Effects: effects})
		tokens = tokens[close+1:]
	}
	return out, len(out) != 0 && len(out)*10 == len(elements)
}

func parseTitlebookSectionEffects(tokens []pvfDefinitionToken) ([]shared.ResourceEffect, bool) {
	var out []shared.ResourceEffect
	for len(tokens) > 0 {
		if tokens[0].kind != 5 {
			return nil, false
		}
		var kinds []byte
		switch tokens[0].text {
		case "[name]":
			kinds = []byte{7}
		case "[image path]":
			kinds = []byte{7, 2}
		case "[give emotion]":
			kinds = []byte{2}
		case "[send postal]":
			kinds = []byte{2, 2, 7, 7, 7}
		default:
			if len(tokens) < 3 {
				return nil, false
			}
			effect, valid := parseTitlebookResourceEffect(tokens[:3])
			if !valid {
				return nil, false
			}
			out = append(out, effect)
			tokens = tokens[3:]
			continue
		}
		if len(tokens) < len(kinds)+1 {
			return nil, false
		}
		for i, kind := range kinds {
			if tokens[i+1].kind != kind {
				return nil, false
			}
		}
		tokens = tokens[len(kinds)+1:]
	}
	return out, true
}
