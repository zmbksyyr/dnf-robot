package pvf

import (
	"encoding/json"
	"math"
	"os"
	"strconv"

	"robot/internal/shared"
)

const pvfCharacterResourceExportName = "character_resource_catalog.json"

var titlebookResourceStats = map[string]int{
	"[hp max]": 13, "[mp max]": 48, "[stuck]": 27, "[good hit]": 33,
	"[move speed]": 11, "[attack speed]": 10, "[cast speed]": 12, "[village moving speed]": 63,
	"[physical attack]": 0, "[physical defense]": 3, "[magical attack]": 1, "[magical defense]": 2,
	"[all element]": 24, "[all element attack]": 46, "[separate attack]": 53, "[mp regen rate]": 29, "[hit recovery]": 30,
}

func loadCharacterResourceCatalog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var definitions shared.CharacterResourceDefinitions
	if err := json.Unmarshal(data, &definitions); err != nil {
		return err
	}
	return shared.SetCharacterResourceDefinitions(&definitions)
}

func extractCharacterResources(archive *pvfArchive) shared.CharacterResourceDefinitions {
	out := shared.CharacterResourceDefinitions{Version: 1}
	if file := archive.files["etc/titlebook.etc"]; file != nil {
		if tokens, valid := archive.definitionTokens(file.Data); valid {
			out.TitlebookDefault, out.TitlebookDefaultKnown = parseTitlebookDefaultResources(tokens)
			out.TitlebookCollection, out.TitlebookCollectionKnown = parseTitlebookCollectionResources(tokens)
		}
	}
	for job, path := range []string{
		"swordman/swordman", "fighter/fighter", "gunner/gunner", "mage/mage", "priest/priest",
		"gunner/atgunner", "thief/thief", "fighter/atfighter", "mage/atmage", "swordman/demonicswordman", "mage/creatormage",
	} {
		file := archive.files["character/"+path+".chr"]
		if file == nil {
			continue
		}
		tokens, valid := archive.definitionTokens(file.Data)
		if !valid {
			continue
		}
		if rows, known := parseWeaponResourceRows(tokens); known {
			out.Weapons = append(out.Weapons, shared.CharacterWeaponResources{Job: job, SkillInfo: rows})
		}
	}
	return out
}

func characterResourceSection(tokens []pvfDefinitionToken, tag string) ([]pvfDefinitionToken, bool) {
	start, end := -1, -1
	for i, token := range tokens {
		if token.kind != 5 {
			continue
		}
		if token.text == "["+tag+"]" {
			if start >= 0 {
				return nil, false
			}
			start = i + 1
		}
		if token.text == "[/"+tag+"]" {
			if start < 0 || end >= 0 {
				return nil, false
			}
			end = i
		}
	}
	if start < 0 || end < start {
		return nil, false
	}
	return tokens[start:end], true
}

func parseTitlebookDefaultResources(tokens []pvfDefinitionToken) ([]shared.ResourceEffect, bool) {
	section, known := characterResourceSection(tokens, "default reward")
	if !known || len(section)%3 != 0 {
		return nil, false
	}
	out := make([]shared.ResourceEffect, 0, len(section)/3)
	for len(section) > 0 {
		effect, valid := parseTitlebookResourceEffect(section[:3])
		if !valid {
			return nil, false
		}
		out = append(out, effect)
		section = section[3:]
	}
	return out, true
}

func parseTitlebookResourceEffect(tokens []pvfDefinitionToken) (shared.ResourceEffect, bool) {
	if len(tokens) != 3 {
		return shared.ResourceEffect{}, false
	}
	tag, mode, value := tokens[0], tokens[1], tokens[2]
	stat, recognized := titlebookResourceStats[tag.text]
	if tag.kind != 5 || !recognized || mode.kind != 7 || mode.text != "+" && mode.text != "%" || value.kind != 2 {
		return shared.ResourceEffect{}, false
	}
	return shared.ResourceEffect{Stat: stat, Percent: mode.text == "%", Value: float32(value.value)}, true
}

func parseWeaponResourceRows(tokens []pvfDefinitionToken) ([][4]float32, bool) {
	section, known := characterResourceSection(tokens, "weapon skill info")
	if !known || len(section) == 0 || len(section)%4 != 0 || len(section) > 256*4 {
		return nil, false
	}
	rows := make([][4]float32, len(section)/4)
	for i, token := range section {
		var value float32
		switch token.kind {
		case 2:
			value = float32(token.value)
		case 4:
			value = math.Float32frombits(uint32(token.value))
		case 0:
			parsed, err := strconv.ParseFloat(token.text, 32)
			if err != nil {
				return nil, false
			}
			value = float32(parsed)
		default:
			return nil, false
		}
		if value < 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, false
		}
		rows[i/4][i%4] = value
	}
	return rows, true
}
