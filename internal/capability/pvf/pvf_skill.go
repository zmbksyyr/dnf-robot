package pvf

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"robot/internal/shared"
)

type SkillState = shared.SkillState

var (
	loadStatePathRE   = regexp.MustCompile(`(?i)^sqr/.+load_state\.nut$`)
	pushStateRE       = regexp.MustCompile(`(?is)IRDSQRCharacter\.pushState\s*\(\s*([^,]+),\s*["']([^"']+)["']\s*,\s*["']([^"']*)["']\s*,\s*([^,]+),\s*([^\)]+)\)`)
	scriptReferenceRE = regexp.MustCompile(`(?i)["']([^"']+\.nut)["']`)
	skillCooldownRE   = regexp.MustCompile(`(?i)\[cool time\]([^\[]*)`)
	skillMPCostRE     = regexp.MustCompile(`(?i)\[consume MP\]([^\[]*)`)
	skillMaxLevelRE   = regexp.MustCompile(`(?i)\[maximum level\]\s*(\d+)`)
)

func setSkillStateCatalog(entries []SkillState) {
	shared.SetSkillStates(entries)
}

func loadSkillStateCatalog(path string) error {
	setSkillStateCatalog(nil)
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
	definitions := extractSkillDefinitions(archive)
	type stateKey struct {
		job, skill, state int
		path              string
	}
	seen := make(map[stateKey]int)
	entries := make([]SkillState, 0)
	for _, text := range loadStates {
		for _, match := range pushStateRE.FindAllStringSubmatch(text, -1) {
			job, jobOK := resolveSkillStateValue(match[1], symbols)
			state, stateOK := resolveSkillStateValue(match[4], symbols)
			skill, skillOK := resolveSkillStateValue(match[5], symbols)
			path := skillScriptPath(match[2])
			if !jobOK || !stateOK || !skillOK || job < 0 || skill <= 0 || skill > 255 || state < 0 || state > 255 || strings.TrimSpace(referenced[path]) == "" {
				continue
			}
			key := stateKey{job: job, skill: skill, state: state, path: path}
			data := scriptEntryStateData(referenced[path], match[3], state, symbols)
			if index, exists := seen[key]; exists {
				// A runtime key can have multiple entry aliases. A constant vector
				// is usable only if every registered alias establishes the same one.
				if (entries[index].StateData == nil) != (data == nil) || !slices.Equal(entries[index].StateData, data) {
					entries[index].StateData = nil
				}
				continue
			}
			seen[key] = len(entries)
			entry := SkillState{Job: job, SkillIndex: skill, State: state, ScriptPath: path}
			entry.StateData = data
			applySkillDefinition(&entry, definitions[[2]int{job, skill}])
			entries = append(entries, entry)
		}
	}
	// These native skills have no Squirrel registration. Original client
	// captures establish each state and vector; still require this archive's
	// matching active definition, rather than trusting the default catalog.
	for _, native := range []struct {
		job, skill, state int
		list, path        string
		data              []int
	}{
		{0, 5, 20, "swordmanskill.lst", "swordman/hardattack.skl", []int{0, 0}},
		{0, 8, 22, "swordmanskill.lst", "swordman/tripleslash.skl", []int{0, 1}},
		{0, 20, 24, "swordmanskill.lst", "swordman/normalwave.skl", []int{20}},
		{0, 46, 28, "swordmanskill.lst", "swordman/upperslash.skl", []int{2, 46}},
		{2, 4, 20, "gunnerskill.lst", "gunner/aerialknee.skl", []int{4}},
		{3, 12, 23, "mageskill.lst", "mage/upperswing.skl", []int{12}},
	} {
		for _, skill := range parsePVFList(archive.text("skill/" + native.list)) {
			if skill.ID != native.skill || skill.Path != native.path {
				continue
			}
			path := "skill/" + skill.Path
			if pvfTagValue(archive.text(path), "type") != "active" {
				continue
			}
			entry := SkillState{Job: native.job, SkillIndex: native.skill, State: native.state, ScriptPath: path, StateData: native.data}
			applySkillDefinition(&entry, definitions[[2]int{native.job, native.skill}])
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

var skillJobLists = [...]string{"swordmanskill.lst", "fighterskill.lst", "gunnerskill.lst", "mageskill.lst", "priestskill.lst",
	"atgunnerskill.lst", "thiefskill.lst", "atfighterskill.lst", "atmageskill.lst", "demonicswordman.lst", "creatormage.lst"}

type skillDefinition struct {
	cooldowns    []int
	baseMP       []int
	mpRules      shared.SkillMPRules
	itemCostFree bool
	materials    shared.SkillMaterialDefinition
}

func extractSkillDefinitions(archive *pvfArchive) map[[2]int]skillDefinition {
	out := make(map[[2]int]skillDefinition)
	for job, list := range skillJobLists {
		for _, entry := range parsePVFList(archive.text("skill/" + list)) {
			text := archive.text("skill/" + entry.Path)
			var tokens []pvfDefinitionToken
			valid := false
			if file := archive.files["skill/"+entry.Path]; file != nil {
				tokens, valid = archive.definitionTokens(file.Data)
			}
			definition := skillDefinition{
				cooldowns:    dungeonSkillCooldowns(text),
				baseMP:       dungeonSkillMPCosts(text),
				mpRules:      parseSkillMPRules(text),
				itemCostFree: skillItemCostFreeFromTokens(tokens, valid),
				materials:    skillMaterialDefinitionFromTokens(tokens, valid),
			}
			key := [2]int{job, entry.ID}
			if previous, exists := out[key]; exists {
				definition.itemCostFree = previous.itemCostFree && definition.itemCostFree
				if !previous.materials.Equal(definition.materials) {
					definition.materials = shared.SkillMaterialDefinition{}
				}
			}
			out[key] = definition
		}
	}
	return out
}

func applySkillDefinition(entry *SkillState, definition skillDefinition) {
	entry.BaseMPByLevel = append([]int(nil), definition.baseMP...)
	entry.MPRules = definition.mpRules
	entry.ItemCostFree = definition.itemCostFree
	entry.Materials = definition.materials.Clone()
	if table := definition.cooldowns; len(table) == 1 {
		entry.CooldownMS = table[0]
	} else {
		entry.CooldownByLevelMS = append([]int(nil), table...)
	}
}

func parseSkillMPRules(text string) shared.SkillMPRules {
	if strings.TrimSpace(text) == "" {
		return shared.SkillMPRules{}
	}
	rules := shared.SkillMPRules{Known: true}
	lines := splitPVFLines(text)
	for i, line := range lines {
		switch strings.ToLower(cleanPVFString(line)) {
		case "weapon effect type":
			switch strings.ToLower(cleanPVFString(nextLine(lines, i))) {
			case "physical":
				rules.WeaponEffect = 1
			case "magical":
				rules.WeaponEffect = 2
			default:
				return shared.SkillMPRules{}
			}
		case "skill command advantage":
			var fields []string
			for _, valueLine := range lines[i+1:] {
				if strings.HasPrefix(valueLine, "[") {
					break
				}
				fields = append(fields, strings.Fields(valueLine)...)
			}
			if len(fields) != 2 {
				return shared.SkillMPRules{}
			}
			for j, field := range fields {
				value, err := strconv.ParseInt(field, 10, 32)
				if err != nil || value < 0 {
					return shared.SkillMPRules{}
				}
				if j == 1 {
					// Native command recognition enables a reduction of this
					// amount * 0.001 from the MP multiplier. Direct skill use
					// does not acquire the command-recognition flag.
					rules.CommandReductionPermille = int(value)
				}
			}
		}
	}
	return rules
}

// Read dungeon cooldowns through the PVF's own job/skill lists. Constant
// values need one entry; variable endpoints use the declared maximum level.
func extractSkillCooldowns(archive *pvfArchive) map[[2]int][]int {
	return extractSkillLevelTables(archive, skillCooldownRE)
}

func extractSkillLevelTables(archive *pvfArchive, field *regexp.Regexp) map[[2]int][]int {
	out := make(map[[2]int][]int)
	for job, list := range skillJobLists {
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
	collectSkillSymbols(scripts, symbols)
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
