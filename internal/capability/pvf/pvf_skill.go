package pvf

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"robot/internal/shared"
)

type SkillState = shared.SkillState

var (
	loadStatePathRE   = regexp.MustCompile(`(?i)^sqr/.+load_state\.nut$`)
	pushStateRE       = regexp.MustCompile(`(?is)IRDSQRCharacter\.pushState\s*\(\s*([^,]+),\s*["']([^"']+)["']\s*,\s*["'][^"']*["']\s*,\s*([^,]+),\s*([^\)]+)\)`)
	scriptReferenceRE = regexp.MustCompile(`(?i)["']([^"']+\.nut)["']`)
	symbolRE          = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Z][A-Z0-9_]*)\s*(?:<-|=)\s*(-?\d+)\s*;?`)
	skillCooldownRE   = regexp.MustCompile(`(?i)\[cool time\]([^\[]*)`)
	skillMPCostRE     = regexp.MustCompile(`(?i)\[consume MP\]([^\[]*)`)
	skillMaxLevelRE   = regexp.MustCompile(`(?i)\[maximum level\]\s*(\d+)`)
)

func setSkillStateCatalog(entries []SkillState) {
	shared.SetSkillStates(entries)
}

func loadSkillStateCatalog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var entries []SkillState
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	setSkillStateCatalog(entries)
	return nil
}

func extractSkillStateCatalog(archive *pvfArchive) []SkillState {
	if archive == nil {
		return nil
	}
	referenced := make(map[string]string)
	loadStates := make([]string, 0)
	for path := range archive.files {
		if !loadStatePathRE.MatchString(path) {
			continue
		}
		text := archive.text(path)
		if strings.TrimSpace(text) == "" {
			continue
		}
		loadStates = append(loadStates, text)
		for _, match := range scriptReferenceRE.FindAllStringSubmatch(text, -1) {
			ref := skillScriptPath(match[1])
			if refText := archive.text(ref); strings.TrimSpace(refText) != "" {
				referenced[ref] = refText
			}
		}
	}
	symbols := skillStateSymbols(referenced)
	cooldowns := extractSkillCooldowns(archive)
	mpCosts := extractSkillLevelTables(archive, skillMPCostRE)
	type stateKey struct {
		job, skill, state int
		path              string
	}
	seen := make(map[stateKey]bool)
	entries := make([]SkillState, 0)
	for _, text := range loadStates {
		for _, match := range pushStateRE.FindAllStringSubmatch(text, -1) {
			job, jobOK := resolveSkillStateValue(match[1], symbols)
			state, stateOK := resolveSkillStateValue(match[3], symbols)
			skill, skillOK := resolveSkillStateValue(match[4], symbols)
			path := skillScriptPath(match[2])
			if !jobOK || !stateOK || !skillOK || job < 0 || skill <= 0 || skill > 255 || state < 0 || state > 255 || strings.TrimSpace(referenced[path]) == "" {
				continue
			}
			key := stateKey{job: job, skill: skill, state: state, path: path}
			if seen[key] {
				continue
			}
			seen[key] = true
			entry := SkillState{Job: job, SkillIndex: skill, State: state, ScriptPath: path}
			entry.BaseMPByLevel = append([]int(nil), mpCosts[[2]int{job, skill}]...)
			if table := cooldowns[[2]int{job, skill}]; len(table) == 1 {
				entry.CooldownMS = table[0]
			} else {
				entry.CooldownByLevelMS = append([]int(nil), table...)
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Job != entries[j].Job {
			return entries[i].Job < entries[j].Job
		}
		if entries[i].SkillIndex != entries[j].SkillIndex {
			return entries[i].SkillIndex < entries[j].SkillIndex
		}
		if entries[i].State != entries[j].State {
			return entries[i].State < entries[j].State
		}
		return entries[i].ScriptPath < entries[j].ScriptPath
	})
	return entries
}

// Read dungeon cooldowns through the PVF's own job/skill lists. Constant
// values need one entry; variable endpoints use the declared maximum level.
func extractSkillCooldowns(archive *pvfArchive) map[[2]int][]int {
	return extractSkillLevelTables(archive, skillCooldownRE)
}

func extractSkillLevelTables(archive *pvfArchive, field *regexp.Regexp) map[[2]int][]int {
	lists := [...]string{"swordmanskill.lst", "fighterskill.lst", "gunnerskill.lst", "mageskill.lst", "priestskill.lst",
		"atgunnerskill.lst", "thiefskill.lst", "atfighterskill.lst", "atmageskill.lst", "demonicswordman.lst", "creatormage.lst"}
	out := make(map[[2]int][]int)
	for job, list := range lists {
		for _, entry := range parsePVFList(archive.text("skill/" + list)) {
			if table := dungeonSkillLevelValues(archive.text("skill/"+entry.Path), field); len(table) > 0 {
				out[[2]int{job, entry.ID}] = table
			}
		}
	}
	return out
}

func dungeonSkillCooldowns(text string) []int {
	return dungeonSkillLevelValues(text, skillCooldownRE)
}

func dungeonSkillMPCosts(text string) []int {
	return dungeonSkillLevelValues(text, skillMPCostRE)
}

func dungeonSkillLevelValues(text string, field *regexp.Regexp) []int {
	// Public values apply unless the dungeon block provides its own table.
	// Other modes cannot supply the public fallback, regardless of order.
	var dungeon []string
	// Some shipped skills leave [dungeon] open through EOF. Remove other
	// modes first so their nested values cannot be mistaken for its override.
	for _, mode := range []string{"pvp", "death tower", "warroom", "dungeon"} {
		open, close := "["+mode+"]", "[/"+mode+"]"
		for {
			start := strings.Index(text, open)
			if start < 0 {
				break
			}
			body := start + len(open)
			end := strings.Index(text[body:], close)
			after := len(text)
			if end < 0 {
				end = len(text)
			} else {
				end += body
				after = end + len(close)
			}
			if mode == "dungeon" {
				if match := field.FindStringSubmatch(text[body:end]); len(match) > 0 {
					dungeon = match
				}
			}
			text = text[:start] + text[after:]
		}
	}
	match := dungeon
	if len(match) == 0 {
		match = field.FindStringSubmatch(text)
	}
	if len(match) == 0 {
		return nil
	}
	fields := strings.Fields(match[1])
	if len(fields) != 2 {
		return nil
	}
	first, e1 := strconv.ParseInt(fields[0], 10, 32)
	last, e2 := strconv.ParseInt(fields[1], 10, 32)
	if e1 != nil || e2 != nil || first < 0 || last < 0 {
		return nil
	}
	if first == last {
		return []int{int(first)}
	}
	maximum := skillMaxLevelRE.FindStringSubmatch(text)
	if len(maximum) == 0 {
		return nil
	}
	count, err := strconv.Atoi(maximum[1])
	if err != nil || count <= 0 || count > 255 {
		return nil
	}
	table := make([]int, count)
	for i := range table {
		if i == count-1 {
			table[i] = int(last)
			continue
		}
		// The client rounds each scalar operation to float32, then truncates
		// the result. Integer interpolation differs even for millisecond data.
		delta := float32(float32(last) - float32(first))
		delta = float32(delta * float32(i))
		delta = float32(delta / float32(count-1))
		table[i] = int(float32(float32(first) + delta))
	}
	return table
}

func skillScriptPath(value string) string {
	value = normalizePVFPath(value)
	if !strings.HasPrefix(value, "sqr/") {
		value = "sqr/" + value
	}
	return value
}

func skillStateSymbols(scripts map[string]string) map[string]int {
	symbols := map[string]int{
		"ENUM_CHARACTERJOB_SWORDMAN":         0,
		"ENUM_CHARACTERJOB_FIGHTER":          1,
		"ENUM_CHARACTERJOB_GUNNER":           2,
		"ENUM_CHARACTERJOB_MAGE":             3,
		"ENUM_CHARACTERJOB_PRIEST":           4,
		"ENUM_CHARACTERJOB_AT_GUNNER":        5,
		"ENUM_CHARACTERJOB_THIEF":            6,
		"ENUM_CHARACTERJOB_AT_FIGHTER":       7,
		"ENUM_CHARACTERJOB_AT_MAGE":          8,
		"ENUM_CHARACTERJOB_DEMONIC_SWORDMAN": 9,
		"ENUM_CHARACTERJOB_CREATOR_MAGE":     10,
	}
	for _, text := range scripts {
		for _, match := range symbolRE.FindAllStringSubmatch(text, -1) {
			value, err := strconv.Atoi(match[2])
			if err == nil {
				if _, exists := symbols[match[1]]; !exists {
					symbols[match[1]] = value
				}
			}
		}
	}
	return symbols
}

func resolveSkillStateValue(raw string, symbols map[string]int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if value, err := strconv.Atoi(raw); err == nil {
		return value, true
	}
	value, ok := symbols[raw]
	return value, ok
}
