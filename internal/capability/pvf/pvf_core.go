package pvf

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"robot/internal/shared"
)

type pvfManifest struct {
	Version           int         `json:"version"`
	SkillStateVersion int         `json:"skill_state_version"`
	Source            string      `json:"source"`
	Size              int64       `json:"size"`
	ModTime           int64       `json:"mod_time"`
	MD5               string      `json:"md5"`
	Runtime           interface{} `json:"runtime,omitempty"`
}

const pvfExportVersion = 1

const pvfSkillStateExportVersion = 2

const pvfItemInfoExportName = "pvf_iteminfo.dat"

const pvfSkillStateExportName = "pvf_skill_state_catalog.json"

func EnsureExports(dfGameR, configDir string) error {
	if configDir == "" {
		return nil
	}
	skillCatalogPath := filepath.Join(configDir, pvfSkillStateExportName)
	if dfGameR == "" {
		_ = loadSkillStateCatalog(skillCatalogPath)
		return nil
	}
	pvfPath := filepath.Join(filepath.Dir(dfGameR), "Script.pvf")
	stat, err := os.Stat(pvfPath)
	if err != nil {
		_ = loadSkillStateCatalog(skillCatalogPath)
		return nil
	}
	manifestPath := filepath.Join(configDir, "pvf_manifest.json")
	metadata := buildPVFManifestMetadata(pvfPath, stat)
	if pvfExportsCurrent(manifestPath, metadata, configDir) {
		if err := loadSkillStateCatalog(skillCatalogPath); err == nil {
			return nil
		}
	}

	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		return err
	}
	if pvfExportsCurrent(manifestPath, manifest, configDir) {
		if err := loadSkillStateCatalog(skillCatalogPath); err == nil {
			return nil
		}
	}

	archive, err := openPVF(pvfPath)
	if err != nil {
		return fmt.Errorf("parse pvf: %w", err)
	}
	equipment, stackable, maps := extractPVFData(archive)
	removeObsoletePVFExports(configDir)
	if err := WriteJSON(filepath.Join(configDir, "pvf_equipment_catalog.json"), equipment); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(configDir, "pvf_stackable_catalog.json"), stackable); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(configDir, "pvf_map_catalog.json"), maps); err != nil {
		return err
	}
	skillStates := extractSkillStateCatalog(archive)
	if err := WriteJSON(filepath.Join(configDir, pvfSkillStateExportName), skillStates); err != nil {
		return err
	}
	setSkillStateCatalog(skillStates)
	if err := writePVFItemInfoExports(configDir, archive, equipment, stackable); err != nil {
		return err
	}
	return WriteJSON(manifestPath, manifest)
}

func buildPVFManifestMetadata(path string, stat os.FileInfo) pvfManifest {
	return pvfManifest{
		Version:           pvfExportVersion,
		SkillStateVersion: pvfSkillStateExportVersion,
		Source:            path,
		Size:              stat.Size(),
		ModTime:           stat.ModTime().Unix(),
	}
}

func buildPVFManifest(path string, stat os.FileInfo) (pvfManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pvfManifest{}, err
	}
	sum := md5.Sum(data)
	manifest := buildPVFManifestMetadata(path, stat)
	manifest.MD5 = hex.EncodeToString(sum[:])
	return manifest, nil
}

func pvfExportsCurrent(manifestPath string, want pvfManifest, configDir string) bool {
	for _, name := range []string{"pvf_equipment_catalog.json", "pvf_stackable_catalog.json", "pvf_map_catalog.json", pvfSkillStateExportName, pvfItemInfoExportName} {
		path := filepath.Join(configDir, name)
		stat, err := os.Stat(path)
		if err != nil || stat.Size() <= 5 {
			return false
		}
		if name == pvfItemInfoExportName {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), `"source_path"`) {
			return false
		}
		if name == "pvf_equipment_catalog.json" && !strings.Contains(string(data), `"item_type": 20`) {
			return false
		}
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	var got pvfManifest
	if json.Unmarshal(data, &got) != nil {
		return false
	}
	md5Matches := false
	if want.MD5 == "" {
		md5Matches = got.MD5 != ""
	} else {
		md5Matches = got.MD5 == want.MD5
	}
	return got.Version == want.Version && got.SkillStateVersion == want.SkillStateVersion && got.Source == want.Source && got.Size == want.Size && got.ModTime == want.ModTime && md5Matches
}

func removeObsoletePVFExports(configDir string) {
	for _, name := range []string{"equipment_catalog.json", "stackable_catalog.json", "map_catalog.json", "pvf_iteminfo_catalog.json"} {
		_ = os.Remove(filepath.Join(configDir, name))
	}
}

func writePVFItemInfoExports(configDir string, archive *pvfArchive, equipment, stackable []shared.EquipmentCatalogItem) error {
	if archive == nil {
		return nil
	}
	text := formatExtendedPVFItemInfoDAT(archive.text("etc/iteminfo.dat"), equipment, stackable)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(configDir, pvfItemInfoExportName), []byte(text), 0644); err != nil {
		return err
	}
	return nil
}

func ExportPVFItemInfoDAT(pvfPath, configDir string) (string, error) {
	if strings.TrimSpace(pvfPath) == "" {
		return "", fmt.Errorf("pvf path is empty")
	}
	if strings.TrimSpace(configDir) == "" {
		return "", fmt.Errorf("config dir is empty")
	}
	archive, err := openPVF(pvfPath)
	if err != nil {
		return "", err
	}
	equipment, stackable, _ := extractPVFData(archive)
	if err := writePVFItemInfoExports(configDir, archive, equipment, stackable); err != nil {
		return "", err
	}
	return filepath.Join(configDir, pvfItemInfoExportName), nil
}

func formatPVFItemInfoDAT(text string) string {
	tokens := tokenizePVFItemInfo(text)
	rows := make([]string, 0, len(tokens)/17)
	for i := 0; i+16 < len(tokens); {
		if tokens[i] == "#PVF_File" {
			i++
			continue
		}
		if _, err := strconv.Atoi(tokens[i]); err != nil {
			i++
			continue
		}
		if _, err := strconv.Atoi(tokens[i+16]); err != nil {
			i++
			continue
		}
		rows = append(rows, strings.Join(tokens[i:i+17], " "))
		i += 17
	}
	if len(rows) == 0 {
		return text
	}
	return strings.Join(rows, "\r\n") + "\r\n"
}

func formatExtendedPVFItemInfoDAT(rawText string, equipment, stackable []shared.EquipmentCatalogItem) string {
	raw := parsePVFItemInfoRows(formatPVFItemInfoDAT(rawText))
	type row struct {
		id   int
		text string
	}
	rows := make([]row, 0, len(raw)+len(equipment)+len(stackable))
	seen := make(map[int]bool, len(raw)+len(equipment)+len(stackable))
	for _, item := range equipment {
		if item.ID <= 0 {
			continue
		}
		rows = append(rows, row{id: item.ID, text: strings.Join(generatedItemInfoFields(item, false), " ")})
		seen[item.ID] = true
	}
	for _, item := range stackable {
		if item.ID <= 0 {
			continue
		}
		rows = append(rows, row{id: item.ID, text: strings.Join(generatedItemInfoFields(item, true), " ")})
		seen[item.ID] = true
	}
	for id, fields := range raw {
		if seen[id] {
			continue
		}
		rows = append(rows, row{id: id, text: strings.Join(fields, " ")})
		seen[id] = true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.text)
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

func parsePVFItemInfoRows(text string) map[int][]string {
	tokens := tokenizePVFItemInfo(text)
	rows := make(map[int][]string, len(tokens)/17)
	for i := 0; i+16 < len(tokens); {
		if tokens[i] == "#PVF_File" {
			i++
			continue
		}
		id, err := strconv.Atoi(tokens[i])
		if err != nil {
			i++
			continue
		}
		if _, err := strconv.Atoi(tokens[i+16]); err != nil {
			i++
			continue
		}
		fields := append([]string(nil), tokens[i:i+17]...)
		rows[id] = fields
		i += 17
	}
	return rows
}

func generatedItemInfoFields(item shared.EquipmentCatalogItem, stackable bool) []string {
	fields := []string{strconv.Itoa(item.ID), strconv.Itoa(nonNegativeInt(item.Rarity))}
	fields = append(fields, generatedItemInfoJobFlags(item, stackable)...)
	level := nonNegativeInt(item.Level)
	if level > 70 {
		level = 70
	}
	fields = append(fields,
		strconv.Itoa(level),
		generatedItemInfoName(item.Name, "item_"+strconv.Itoa(item.ID)),
		generatedItemInfoName(item.Name2, "name2_"+strconv.Itoa(item.ID)),
		strconv.Itoa(generatedItemInfoCategory(item, stackable)),
	)
	return fields
}

func generatedItemInfoJobFlags(item shared.EquipmentCatalogItem, stackable bool) []string {
	flags := make([]string, 11)
	for i := range flags {
		flags[i] = "1"
	}
	return flags
}

func generatedItemInfoName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "ErrorString") {
		value = fallback
	}
	value = strings.ReplaceAll(value, "`", "'")
	return "`" + value + "`"
}

func generatedItemInfoCategory(item shared.EquipmentCatalogItem, stackable bool) int {
	if stackable {
		return generatedStackableItemInfoCategory(item)
	}
	return generatedEquipmentItemInfoCategory(item)
}

func generatedEquipmentItemInfoCategory(item shared.EquipmentCatalogItem) int {
	path := normalizePVFPath(item.Path)
	parts := strings.Split(path, "/")
	slot := strings.ToLower(item.Slot)
	if slot == "weapon" {
		return 10000 + equipmentCharacterCategoryCode(parts)*100 + equipmentWeaponCategorySuffix(parts, item)
	}
	if armorClass := armorCategoryClass(parts); armorClass >= 0 {
		if suffix := armorCategorySuffix(slot, armorClass); suffix > 0 {
			if armorClass == 0 {
				return 11000 + suffix
			}
			return 11000 + armorClass*100 + suffix
		}
	}
	switch slot {
	case "amulet":
		return 12001
	case "ring":
		return 12002
	case "wrist":
		return 12003
	case "titlename", "title", "title name":
		return 12004
	case "creature":
		return 14001
	case "artifact red":
		return 14002
	case "artifact blue":
		return 14003
	case "artifact green":
		return 14004
	case "support":
		return 32001
	case "magic stone":
		return 32100
	}
	if strings.Contains(slot, "avatar") {
		return generatedAvatarCategory(parts, slot)
	}
	return 11000 + nonNegativeInt(item.ItemType)
}

func generatedStackableItemInfoCategory(item shared.EquipmentCatalogItem) int {
	path := normalizePVFPath(item.Path)
	slot := strings.ToLower(item.Slot)
	switch {
	case strings.HasPrefix(path, "stackable/cash/"):
		return 13001
	case strings.HasPrefix(path, "stackable/recipe/") || strings.Contains(slot, "recipe"):
		return 31305
	case strings.HasPrefix(path, "stackable/monstercard/") || strings.Contains(slot, "material expert job"):
		return 33004
	case strings.HasPrefix(path, "stackable/professional/bead/") || strings.Contains(slot, "enchant waste"):
		return 33003
	case strings.HasPrefix(path, "stackable/material/") || strings.Contains(slot, "material"):
		return 13002
	default:
		return 13006
	}
}

func equipmentCharacterCategoryCode(parts []string) int {
	for i, part := range parts {
		if part == "character" || part == "character21" {
			if i+1 >= len(parts) {
				break
			}
			switch parts[i+1] {
			case "swordman":
				return 1
			case "fighter":
				return 2
			case "gunner":
				return 3
			case "mage":
				return 4
			case "priest":
				return 5
			case "thief":
				return 6
			}
		}
	}
	return 0
}

func equipmentWeaponCategorySuffix(parts []string, item shared.EquipmentCatalogItem) int {
	for i, part := range parts {
		if part != "weapon" || i+1 >= len(parts) {
			continue
		}
		switch parts[i+1] {
		case "dagger":
			return 2
		case "twinsword":
			return 3
		case "wand":
			return 4
		case "beamsword":
			return 6
		}
	}
	return nonNegativeInt(item.SubType) + 2
}

func armorCategoryClass(parts []string) int {
	for _, part := range parts {
		switch part {
		case "cloth":
			return 0
		case "leather":
			return 1
		case "larmor":
			return 2
		case "harmor":
			return 3
		case "plate":
			return 4
		}
	}
	return -1
}

func armorCategorySuffix(slot string, armorClass int) int {
	switch slot {
	case "coat":
		if armorClass == 0 {
			return 2
		}
		return 1
	case "shoulder":
		if armorClass == 0 {
			return 3
		}
		return 2
	case "pants":
		if armorClass == 0 {
			return 4
		}
		return 3
	case "shoes":
		if armorClass == 0 {
			return 5
		}
		return 4
	case "waist":
		if armorClass == 0 {
			return 6
		}
		return 5
	default:
		return 0
	}
}

func generatedAvatarCategory(parts []string, slot string) int {
	charCode := equipmentCharacterCategoryCode(parts)
	if charCode <= 0 {
		charCode = 1
	}
	slotSuffix := map[string]int{
		"hatavatar":    2,
		"hairavatar":   3,
		"faceavatar":   4,
		"coatavatar":   5,
		"pantsavatar":  6,
		"shoesavatar":  7,
		"breastavatar": 8,
		"waistavatar":  9,
		"skinavatar":   10,
	}[slot]
	if slotSuffix == 0 {
		slotSuffix = 1
	}
	for _, part := range parts {
		if part == "at_avatar" {
			return 15000 + slotSuffix
		}
	}
	return 23000 + charCode*100 + slotSuffix
}

func nonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func tokenizePVFItemInfo(text string) []string {
	tokens := make([]string, 0, 1024)
	for i := 0; i < len(text); {
		for i < len(text) && isPVFSpace(text[i]) {
			i++
		}
		if i >= len(text) {
			break
		}
		if text[i] == '`' {
			start := i
			i++
			for i < len(text) && text[i] != '`' {
				i++
			}
			if i < len(text) {
				i++
			}
			tokens = append(tokens, text[start:i])
			continue
		}
		start := i
		for i < len(text) && !isPVFSpace(text[i]) {
			i++
		}
		tokens = append(tokens, text[start:i])
	}
	return tokens
}

func isPVFSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
