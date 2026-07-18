package pvf

import (
	"encoding/json"
	"os"
	"regexp"
	"robot/internal/shared"
	"sort"
	"strconv"
	"strings"
)

type SkillState = shared.SkillState

var (
	loadStatePathRE   = regexp.MustCompile(`(?i)^sqr/character/.+load_state\.nut$`)
	pushStateRE       = regexp.MustCompile(`(?is)IRDSQRCharacter\.pushState\s*\(\s*([^,]+),\s*["']([^"']+)["']\s*,\s*["'][^"']*["']\s*,\s*([^,]+),\s*([^\)]+)\)`)
	scriptReferenceRE = regexp.MustCompile(`(?i)["'](character/[^"']+\.nut)["']`)
	symbolRE          = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Z][A-Z0-9_]*)\s*(?:<-|=)\s*(-?\d+)\s*;?`)
	stateHandlerRE    = regexp.MustCompile(`(?is)function\s+on(?:After)?SetState_\w+\s*\(`)
	checkSkillRE      = regexp.MustCompile(`(?is)function\s+checkExecutableSkill_\w+\s*\([^)]*\)\s*\{`)
	nextFunctionRE    = regexp.MustCompile(`(?is)\n\s*function\s+\w+\s*\(`)
	intVectPushRE     = regexp.MustCompile(`(?is)sq_IntVectPush\s*\(([^)]*)\)`)
	intVectClearRE    = regexp.MustCompile(`(?is)sq_IntVectClear\s*\(`)
	addSetStateRE     = regexp.MustCompile(`(?is)sq_AddSetStatePacket\s*\(`)
	targetSkillRE     = regexp.MustCompile(`(?i)(getmyactiveobject|getmypassiveobject|findtarget|getobject|isenemy|collision|isholdable|throw|grab|hold|catch|rope)`)
	spawnSkillRE      = regexp.MustCompile(`(?i)(passiveobject|appendage|createobject|sq_spawn|setcustomdata)`)
)

func SkillStatesForJob(job int) []SkillState {
	return shared.SkillStatesForJob(job)
}

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
	loadStates := make(map[string]string)
	for path := range archive.files {
		if !loadStatePathRE.MatchString(path) {
			continue
		}
		text := archive.text(path)
		if strings.TrimSpace(text) == "" {
			continue
		}
		loadStates[path] = text
		for _, match := range scriptReferenceRE.FindAllStringSubmatch(text, -1) {
			ref := normalizePVFPath("sqr/" + match[1])
			if refText := archive.text(ref); strings.TrimSpace(refText) != "" {
				referenced[ref] = refText
			}
		}
	}
	symbols := skillStateSymbols(referenced)
	seen := make(map[[3]int]bool)
	entries := make([]SkillState, 0)
	for _, text := range loadStates {
		for _, match := range pushStateRE.FindAllStringSubmatch(text, -1) {
			job, jobOK := resolveSkillStateValue(match[1], symbols)
			state, stateOK := resolveSkillStateValue(match[3], symbols)
			skill, skillOK := resolveSkillStateValue(match[4], symbols)
			path := normalizePVFPath("sqr/" + match[2])
			script := referenced[path]
			stateData, verified := verifiedSkillStateData(job, skill, state, path)
			experimentData, experimental, risk := experimentalSkillStateData(script, symbols)
			if !verified && experimental {
				stateData = experimentData
			}
			if !jobOK || !stateOK || !skillOK || job < 0 || skill <= 0 || skill > 255 || state < 0 || state > 255 || (!verified && !experimental && !skillStateScriptUsesEmptyData(script)) {
				continue
			}
			key := [3]int{job, skill, state}
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, SkillState{Job: job, SkillIndex: skill, State: state, ScriptPath: path, StateData: stateData, Verified: verified, Experimental: experimental, Risk: risk})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Job != entries[j].Job {
			return entries[i].Job < entries[j].Job
		}
		if entries[i].SkillIndex != entries[j].SkillIndex {
			return entries[i].SkillIndex < entries[j].SkillIndex
		}
		return entries[i].State < entries[j].State
	})
	return entries
}

func verifiedSkillStateData(job, skill, state int, path string) ([]byte, bool) {
	if job == 6 && skill == 3 && state == 22 && strings.HasSuffix(path, "/thief/1_rogue/shiningcut/shiningcut.nut") {
		return []byte{0x03, 0x00, 0x00}, true
	}
	return nil, false
}

func experimentalSkillStateData(script string, symbols map[string]int) ([]byte, bool, int) {
	if !stateHandlerRE.MatchString(script) || targetSkillRE.MatchString(script) {
		return nil, false, 0
	}
	body, ok := firstCheckExecutableSkillBody(script)
	if !ok {
		return nil, false, 0
	}
	add := addSetStateRE.FindStringIndex(body)
	if add == nil {
		return nil, false, 0
	}
	prefix := body[:add[0]]
	if clears := intVectClearRE.FindAllStringIndex(prefix, -1); len(clears) > 0 {
		prefix = prefix[clears[len(clears)-1][1]:]
	}
	values := make([]int, 0, 3)
	for _, match := range intVectPushRE.FindAllStringSubmatch(prefix, -1) {
		value, ok := resolveSkillStateValue(match[1], symbols)
		if !ok || value < 0 || value > 0xffffff {
			return nil, false, 0
		}
		values = append(values, value)
	}
	if len(values) > 3 {
		return nil, false, 0
	}
	data := make([]byte, 0, len(values)*3)
	for _, value := range values {
		data = append(data, byte(value), byte(value>>8), byte(value>>16))
	}
	risk := 1
	if spawnSkillRE.MatchString(script) {
		risk = 2
	}
	return data, true, risk
}

func firstCheckExecutableSkillBody(script string) (string, bool) {
	match := checkSkillRE.FindStringIndex(script)
	if match == nil {
		return "", false
	}
	body := script[match[1]:]
	if next := nextFunctionRE.FindStringIndex(body); next != nil {
		body = body[:next[0]]
	}
	return body, true
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

func skillStateScriptUsesEmptyData(script string) bool {
	if strings.TrimSpace(script) == "" {
		return false
	}
	body := strings.ToLower(script)
	return stateHandlerRE.MatchString(script) && !strings.Contains(body, "sq_getvectordata")
}
