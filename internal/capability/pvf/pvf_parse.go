package pvf

import (
	"crypto/md5"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"

	"robot/internal/shared"
)

func deriveItemSetKey(path, body string, item shared.EquipmentCatalogItem) string {
	if item.ItemType <= 0 {
		return ""
	}
	if key := explicitPVFSetKey(body); key != "" {
		return "pvf_" + shortHash(key)
	}
	if key := nameSetKey(body, item.ItemType); key != "" {
		return "name_" + shortHash(key)
	}
	if key := pathSetKey(path, item.ItemType); key != "" {
		return "path_" + shortHash(key)
	}
	return ""
}

func explicitPVFSetKey(body string) string {
	lines := splitPVFLines(body)
	for i, line := range lines {
		lower := strings.ToLower(cleanPVFString(line))
		if !strings.Contains(lower, "set") {
			continue
		}
		switch lower {
		case "[set item]", "[set]", "[set name]", "[equipment set]":
			if value := cleanPVFString(nextLine(lines, i)); value != "" && !strings.EqualFold(value, "ErrorString") {
				return lower + ":" + value
			}
		default:
			if i+1 < len(lines) {
				value := cleanPVFString(lines[i+1])
				if value != "" && !strings.HasPrefix(value, "[") && !strings.EqualFold(value, "ErrorString") {
					return lower + ":" + value
				}
			}
		}
	}
	return ""
}

func pathSetKey(path string, itemType int) string {
	p := strings.TrimSuffix(normalizePVFPath(path), filepath.Ext(path))
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return ""
	}
	parts = parts[:len(parts)-1]
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || itemTypePathSegment(part) {
			continue
		}
		out = append(out, part)
	}
	if len(out) < 2 {
		return ""
	}
	return strings.Join(out, "/")
}

func nameSetKey(body string, itemType int) string {
	lines := splitPVFLines(body)
	for i, line := range lines {
		if line != "[name]" {
			continue
		}
		name := cleanPVFString(nextLine(lines, i))
		if name == "" || strings.EqualFold(name, "ErrorString") {
			return ""
		}
		name = strings.ToLower(name)
		for _, word := range itemTypeWords(itemType) {
			name = strings.ReplaceAll(name, word, "")
		}
		name = strings.Join(strings.Fields(name), "")
		if len([]rune(name)) < 2 {
			return ""
		}
		return name
	}
	return ""
}

func itemTypePathSegment(part string) bool {
	switch strings.ToLower(part) {
	case "weapon", "titlename", "title", "coat", "shoulder", "pants", "shoes", "waist", "belt",
		"amulet", "necklace", "wrist", "bracelet", "ring", "support", "magicstone", "magic stone", "magic_stone",
		"cap", "hat", "hair", "face", "neck", "skin", "aura",
		"artifact", "redartifact", "blueartifact", "greenartifact", "red_artifact", "blue_artifact", "green_artifact":
		return true
	default:
		return false
	}
}

func itemTypeWords(itemType int) []string {
	switch itemType {
	case 1:
		return []string{"weapon"}
	case 2:
		return []string{"title", "titlename", "title name"}
	case 3, 23:
		return []string{"coat"}
	case 4:
		return []string{"shoulder"}
	case 5, 24:
		return []string{"pants"}
	case 6, 25:
		return []string{"shoes"}
	case 7, 27:
		return []string{"waist", "belt"}
	case 8:
		return []string{"amulet", "necklace"}
	case 9:
		return []string{"wrist", "bracelet"}
	case 10:
		return []string{"ring"}
	case 11:
		return []string{"support"}
	case 12:
		return []string{"magicstone", "magic stone", "magic_stone"}
	case 20:
		return []string{"cap", "hat"}
	case 21:
		return []string{"hair"}
	case 22:
		return []string{"face"}
	case 26:
		return []string{"neck", "breast"}
	case 28:
		return []string{"skin"}
	case 29:
		return []string{"aura"}
	case 31:
		return []string{"artifact red", "redartifact", "red_artifact"}
	case 32:
		return []string{"artifact blue", "blueartifact", "blue_artifact"}
	case 33:
		return []string{"artifact green", "greenartifact", "green_artifact"}
	default:
		return nil
	}
}

func shortHash(value string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(value))))
	return hex.EncodeToString(sum[:])[:10]
}

func jobFromEquipmentPath(path string) int {
	p := "/" + strings.ToLower(normalizePVFPath(path)) + "/"
	switch {
	case strings.Contains(p, "/character/swordman/"):
		return 0
	case strings.Contains(p, "/character/fighter/at_avatar/"):
		return 7
	case strings.Contains(p, "/character/fighter/"):
		return 1
	case strings.Contains(p, "/character/gunner/at_avatar/"):
		return 5
	case strings.Contains(p, "/character/gunner/"):
		return 2
	case strings.Contains(p, "/character/mage/at_avatar/"):
		return 8
	case strings.Contains(p, "/character/mage/"):
		return 3
	case strings.Contains(p, "/character/priest/at_avatar/"):
		return 14
	case strings.Contains(p, "/character/priest/"):
		return 4
	case strings.Contains(p, "/character/thief/"):
		return 6
	case strings.Contains(p, "/character/demonicswordman/"):
		return 9
	default:
		return -1
	}
}

func applyEquipmentPathJob(item *shared.EquipmentCatalogItem, pathJob int) {
	if item == nil || pathJob < 0 {
		return
	}
	if item.ItemType >= 20 && item.ItemType <= 29 {
		item.UseJob = []int{pathJob}
		return
	}
	if len(item.UseJob) == 0 {
		item.UseJob = []int{pathJob}
	}
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func equipmentTypeFromPath(path string) (int, string) {
	p := "/" + strings.ToLower(normalizePVFPath(path)) + "/"
	if !strings.Contains(p, "/avatar/") && !strings.Contains(p, "/at_avatar/") {
		return 0, ""
	}
	switch {
	case strings.Contains(p, "/cap/"):
		return 20, "hatavatar"
	case strings.Contains(p, "/hair/"):
		return 21, "hairavatar"
	case strings.Contains(p, "/face/"):
		return 22, "faceavatar"
	case strings.Contains(p, "/coat/"):
		return 23, "coatavatar"
	case strings.Contains(p, "/pants/"):
		return 24, "pantsavatar"
	case strings.Contains(p, "/shoes/"):
		return 25, "shoesavatar"
	case strings.Contains(p, "/neck/"):
		return 26, "breastavatar"
	case strings.Contains(p, "/belt/"):
		return 27, "waistavatar"
	case strings.Contains(p, "/skin/"):
		return 28, "skinavatar"
	case strings.Contains(p, "/aura/"):
		return 29, "auroraavatar"
	default:
		return 0, ""
	}
}

type pvfListEntry struct {
	ID   int
	Path string
}

func parsePVFList(text string) []pvfListEntry {
	rawFields := strings.Fields(strings.ReplaceAll(text, "`", ""))
	fields := make([]string, 0, len(rawFields))
	for _, field := range rawFields {
		if strings.EqualFold(field, "#PVF_File") {
			continue
		}
		fields = append(fields, field)
	}
	var out []pvfListEntry
	for i := 0; i+1 < len(fields); i += 2 {
		id := atoi(fields[i])
		path := normalizePVFPath(fields[i+1])
		if id > 0 && path != "" {
			out = append(out, pvfListEntry{ID: id, Path: path})
		}
	}
	return out
}

func parseAreas(body string) []int {
	var out []int
	start := 0
	for {
		i := strings.Index(body[start:], "[area]")
		if i < 0 {
			break
		}
		i += start
		j := strings.Index(body[i:], "[/area]")
		if j < 0 {
			break
		}
		j += i
		block := body[i+len("[area]") : j]
		if !strings.Contains(block, "[gate]") {
			fields := strings.Fields(block)
			if len(fields) > 0 {
				if id := atoi(fields[0]); id >= 0 {
					out = append(out, id)
				}
			}
		}
		start = j + len("[/area]")
	}
	if len(out) == 0 {
		out = []int{0}
	}
	return out
}

func parseJobs(text string) []int {
	seen := map[int]bool{}
	var out []int
	add := func(name string) {
		id := jobID(cleanPVFString(name))
		if id >= 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rest := text
	for {
		start := strings.Index(rest, "[")
		if start < 0 {
			break
		}
		end := strings.Index(rest[start+1:], "]")
		if end < 0 {
			break
		}
		add(rest[start+1 : start+1+end])
		rest = rest[start+1+end+1:]
	}
	if len(out) > 0 {
		return out
	}
	fields := strings.Fields(cleanPVFString(text))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if i+1 < len(fields) {
			pair := field + " " + fields[i+1]
			before := len(out)
			add(pair)
			if len(out) > before {
				i++
				continue
			}
		}
		add(field)
	}
	return out
}

func parseStackableTradeBlock(body string, item *shared.EquipmentCatalogItem) {
	if item == nil {
		return
	}
	block := section(body, "[trade]", "[/trade]")
	if strings.TrimSpace(block) == "" {
		return
	}
	item.TradeBlock = true
	if value, ok := parsePVFBoolFlag(block, "available trade"); ok {
		item.CanTrade = &value
		if value {
			item.Trade = true
		} else {
			item.NoTrade = true
		}
	}
	if value, ok := parsePVFBoolFlag(block, "available auction"); ok {
		item.CanAuction = &value
		item.Auction = value
	}
	if value, ok := parsePVFBoolFlag(block, "available shop"); ok {
		item.CanShop = &value
		item.Shop = value
	}
	if value, ok := parsePVFBoolFlag(block, "available drop"); ok {
		item.CanDrop = &value
	}
}

func parsePVFBoolFlag(block, name string) (bool, bool) {
	lines := splitPVFLines(block)
	name = strings.ToLower(strings.TrimSpace(name))
	for i, line := range lines {
		cleaned := strings.ToLower(cleanPVFString(line))
		if !strings.Contains(cleaned, name) {
			continue
		}
		fields := strings.Fields(cleaned)
		for idx, field := range fields {
			if field == "0" {
				return false, true
			}
			if field == "1" {
				return true, true
			}
			if strings.Contains(field, name) && idx+1 < len(fields) {
				return atoi(fields[idx+1]) != 0, true
			}
		}
		if i+1 < len(lines) {
			return atoi(lines[i+1]) != 0, true
		}
		return false, true
	}
	return false, false
}

func jobID(job string) int {
	switch strings.ToLower(strings.ReplaceAll(job, " ", "")) {
	case "all":
		return 100
	case "swordman":
		return 0
	case "fighter":
		return 1
	case "gunner":
		return 2
	case "mage":
		return 3
	case "priest":
		return 4
	case "atpriest":
		return 14
	case "atgunner":
		return 5
	case "thief":
		return 6
	case "atfighter":
		return 7
	case "atmage":
		return 8
	case "demonicswordman":
		return 9
	case "creatormage":
		return 10
	default:
		return -1
	}
}

func equipmentType(v string) int {
	switch normalizeEquipmentTypeName(v) {
	case "weapon":
		return 1
	case "titlename", "title", "title name":
		return 2
	case "coat":
		return 3
	case "shoulder":
		return 4
	case "pants":
		return 5
	case "shoes":
		return 6
	case "waist":
		return 7
	case "amulet":
		return 8
	case "wrist":
		return 9
	case "ring":
		return 10
	case "support":
		return 11
	case "magicstone", "magic stone", "magic_stone":
		return 12
	case "hatavatar":
		return 20
	case "hairavatar":
		return 21
	case "faceavatar":
		return 22
	case "coatavatar":
		return 23
	case "pantsavatar":
		return 24
	case "shoesavatar":
		return 25
	case "breastavatar":
		return 26
	case "waistavatar":
		return 27
	case "skinavatar":
		return 28
	case "auroraavatar":
		return 29
	case "creature":
		return 30
	case "artifactred":
		return 31
	case "artifactblue":
		return 32
	case "artifactgreen":
		return 33
	default:
		return -1
	}
}

func normalizeEquipmentTypeName(v string) string {
	v = strings.ToLower(cleanPVFString(v))
	v = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(v)
	switch v {
	case "redartifact", "creatureartifactred", "creatureredartifact", "petartifactred", "petredartifact":
		return "artifactred"
	case "blueartifact", "creatureartifactblue", "creatureblueartifact", "petartifactblue", "petblueartifact":
		return "artifactblue"
	case "greenartifact", "creatureartifactgreen", "creaturegreenartifact", "petartifactgreen", "petgreenartifact":
		return "artifactgreen"
	default:
		return v
	}
}

func section(text, startTag, endTag string) string {
	start := strings.Index(text, startTag)
	if start < 0 {
		return ""
	}
	start += len(startTag)
	end := strings.Index(text[start:], endTag)
	if end < 0 {
		return ""
	}
	return text[start : start+end]
}

func splitPVFLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", "\n")
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func nextLine(lines []string, i int) string {
	if i+1 >= len(lines) {
		return ""
	}
	return lines[i+1]
}

func cleanPVFString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`[]\x00")
	return strings.TrimSpace(s)
}

func cleanPVFTableString(s string) string {
	s = strings.TrimSpace(strings.TrimRight(s, "\x00"))
	return strings.TrimSpace(s)
}

func normalizePVFPath(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "`\x00"))
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.ToLower(s)
}

func atoi(s string) int {
	s = cleanPVFString(s)
	n, _ := strconv.Atoi(s)
	return n
}
