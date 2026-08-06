package pvf

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"robot/internal/capability/catalog"
	"robot/internal/foundation/lockhub"
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

const pvfItemInfoExportName = "iteminfo.dat"

const pvfEquipmentExportName = "equipment_catalog.json"

const pvfStackableExportName = "stackable_catalog.json"

const pvfMapExportName = "map_catalog.json"

const pvfSkillStateExportName = "skill_state_catalog.json"

const pvfLevelExpExportName = "level_exp_catalog.json"

const pvfManifestName = "pvf_manifest.json"

const (
	pvfMarkerScanChunk   = 64 * 1024
	pvfMarkerScanOverlap = 64
)

var (
	pvfLegacySourceMarker   = []byte(`"source_path"`)
	pvfEquipmentMarkers     = [][]byte{[]byte(`"item_type": 20`)}
	pvfMapEligibilityMarker = [][]byte{[]byte(`"normal_eligible"`), []byte(`"store_eligible"`)}
)

var exportMu lockhub.Locker

func EnsureExports(dfGameR, pvfDir string, tempDirs ...string) error {
	exportMu.Lock()
	defer exportMu.Unlock()
	return ensureExports(dfGameR, pvfDir, firstTempDir(pvfDir, tempDirs))
}

func ensureExports(dfGameR, pvfDir, tempDir string) error {
	if pvfDir == "" {
		return nil
	}
	if err := recoverPVFPublish(pvfDir, tempDir); err != nil {
		return err
	}
	skillCatalogPath := filepath.Join(pvfDir, pvfSkillStateExportName)
	levelExpPath := filepath.Join(pvfDir, pvfLevelExpExportName)
	if dfGameR == "" {
		_ = loadSkillStateCatalog(skillCatalogPath)
		_ = loadLevelExpCatalog(levelExpPath)
		return nil
	}
	pvfPath := filepath.Join(filepath.Dir(dfGameR), "Script.pvf")
	stat, err := os.Stat(pvfPath)
	if err != nil {
		_ = loadSkillStateCatalog(skillCatalogPath)
		_ = loadLevelExpCatalog(levelExpPath)
		return nil
	}
	manifestPath := filepath.Join(pvfDir, pvfManifestName)
	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		return err
	}
	if pvfExportsCurrent(manifestPath, manifest, pvfDir) {
		if err := loadSkillStateCatalog(skillCatalogPath); err == nil && loadLevelExpCatalog(levelExpPath) == nil {
			return nil
		}
	}

	archive, err := openPVF(pvfPath)
	if err != nil {
		return fmt.Errorf("parse pvf: %w", err)
	}
	equipment, stackable, maps := extractPVFData(archive)
	levelExp, err := extractPVFLevelExp(archive)
	if err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp(tempDir, "pvf-export-")
	if err != nil {
		return fmt.Errorf("create PVF export staging directory: %w", err)
	}
	defer os.RemoveAll(stageDir)
	if err := WriteJSON(filepath.Join(stageDir, pvfEquipmentExportName), equipment); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(stageDir, pvfStackableExportName), stackable); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(stageDir, pvfMapExportName), maps); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(stageDir, pvfLevelExpExportName), levelExp); err != nil {
		return err
	}
	skillStates := extractSkillStateCatalog(archive)
	if err := WriteJSON(filepath.Join(stageDir, pvfSkillStateExportName), skillStates); err != nil {
		return err
	}
	if err := writePVFItemInfoExports(stageDir, archive, equipment, stackable); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(stageDir, pvfManifestName), manifest); err != nil {
		return err
	}
	if err := publishPVFDirectory(stageDir, pvfDir, tempDir); err != nil {
		return err
	}
	if err := loadSkillStateCatalog(skillCatalogPath); err != nil {
		return fmt.Errorf("load published PVF skill states: %w", err)
	}
	if err := loadLevelExpCatalog(levelExpPath); err != nil {
		return fmt.Errorf("load published PVF level experience: %w", err)
	}
	return nil
}

func firstTempDir(pvfDir string, tempDirs []string) string {
	if len(tempDirs) > 0 && strings.TrimSpace(tempDirs[0]) != "" {
		return tempDirs[0]
	}
	return filepath.Join(filepath.Dir(pvfDir), "tmp")
}

func recoverPVFPublish(pvfDir, tempDir string) error {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return err
	}
	staleStages, err := filepath.Glob(filepath.Join(tempDir, "pvf-export-*"))
	if err != nil {
		return fmt.Errorf("find interrupted PVF staging directories: %w", err)
	}
	for _, stageDir := range staleStages {
		if err := os.RemoveAll(stageDir); err != nil {
			return fmt.Errorf("remove interrupted PVF staging directory %s: %w", stageDir, err)
		}
	}
	backupDir := filepath.Join(tempDir, "pvf.previous")
	_, outputErr := os.Stat(pvfDir)
	_, backupErr := os.Stat(backupDir)
	if os.IsNotExist(outputErr) && backupErr == nil {
		if err := os.Rename(backupDir, pvfDir); err != nil {
			return fmt.Errorf("restore interrupted PVF publish: %w", err)
		}
		return nil
	}
	if outputErr == nil && backupErr == nil {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove completed PVF publish backup: %w", err)
		}
	}
	return nil
}

func publishPVFDirectory(stageDir, pvfDir, tempDir string) error {
	if info, err := os.Stat(stageDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return fmt.Errorf("invalid PVF export staging directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(pvfDir), 0755); err != nil {
		return err
	}
	backupDir := filepath.Join(tempDir, "pvf.previous")
	if err := os.RemoveAll(backupDir); err != nil {
		return err
	}
	hadPrevious := false
	if _, err := os.Stat(pvfDir); err == nil {
		if err := os.Rename(pvfDir, backupDir); err != nil {
			return fmt.Errorf("stage previous PVF export: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stageDir, pvfDir); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backupDir, pvfDir); restoreErr != nil {
				return fmt.Errorf("publish PVF export: %v; restore previous export: %w", err, restoreErr)
			}
		}
		return fmt.Errorf("publish PVF export: %w", err)
	}
	if hadPrevious {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove previous PVF export: %w", err)
		}
	}
	return nil
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
	file, err := os.Open(path)
	if err != nil {
		return pvfManifest{}, err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return pvfManifest{}, err
	}
	manifest := buildPVFManifestMetadata(path, stat)
	manifest.MD5 = hex.EncodeToString(hash.Sum(nil))
	return manifest, nil
}

func pvfExportsCurrent(manifestPath string, want pvfManifest, configDir string) bool {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	var got pvfManifest
	if json.Unmarshal(data, &got) != nil || want.MD5 == "" || got.MD5 != want.MD5 || got.Version != want.Version || got.SkillStateVersion != want.SkillStateVersion || got.Source != want.Source || got.Size != want.Size || got.ModTime != want.ModTime {
		return false
	}

	markerScratch := make([]byte, pvfMarkerScanChunk+pvfMarkerScanOverlap)
	for _, name := range []string{pvfEquipmentExportName, pvfStackableExportName, pvfMapExportName, pvfSkillStateExportName, pvfLevelExpExportName, pvfItemInfoExportName} {
		path := filepath.Join(configDir, name)
		stat, err := os.Stat(path)
		if err != nil || stat.Size() <= 5 {
			return false
		}
		if name == pvfItemInfoExportName || name == pvfSkillStateExportName || name == pvfLevelExpExportName {
			continue
		}
		var required [][]byte
		switch name {
		case pvfEquipmentExportName:
			required = pvfEquipmentMarkers
		case pvfMapExportName:
			required = pvfMapEligibilityMarker
		}
		if !pvfExportMarkersCurrent(path, required, markerScratch) {
			return false
		}
	}
	return true
}

func pvfExportMarkersCurrent(path string, required [][]byte, scratch []byte) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	maxMarker := len(pvfLegacySourceMarker)
	for _, marker := range required {
		if len(marker) > maxMarker {
			maxMarker = len(marker)
		}
	}
	if len(required) > 64 || len(scratch) < pvfMarkerScanChunk+maxMarker {
		return false
	}
	requiredMask := uint64(1)<<len(required) - 1
	foundMask := uint64(0)
	carry := 0
	for {
		n, readErr := file.Read(scratch[carry : carry+pvfMarkerScanChunk])
		chunk := scratch[:carry+n]
		if bytes.Contains(chunk, pvfLegacySourceMarker) {
			return false
		}
		for index, marker := range required {
			if foundMask&(uint64(1)<<index) == 0 && bytes.Contains(chunk, marker) {
				foundMask |= uint64(1) << index
			}
		}
		if readErr == io.EOF {
			return foundMask == requiredMask
		}
		if readErr != nil {
			return false
		}
		carry = maxMarker - 1
		if carry > len(chunk) {
			carry = len(chunk)
		}
		copy(scratch[:carry], chunk[len(chunk)-carry:])
	}
}

func loadLevelExpCatalog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var values []int
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	return catalog.SetLevelMinExpTable(values)
}

func writePVFItemInfoExports(configDir string, archive *pvfArchive, equipment, stackable []shared.EquipmentCatalogItem) error {
	if archive == nil {
		return nil
	}
	text := formatExtendedPVFItemInfoDAT(archive.text("etc/iteminfo.dat"), equipment, stackable)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := writeFileAtomic(filepath.Join(configDir, pvfItemInfoExportName), []byte(text), 0644); err != nil {
		return err
	}
	return nil
}

func ExportPVFItemInfoDAT(pvfPath, configDir string) (string, error) {
	exportMu.Lock()
	defer exportMu.Unlock()
	return exportPVFItemInfoDAT(pvfPath, configDir)
}

func exportPVFItemInfoDAT(pvfPath, configDir string) (string, error) {
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

func EnsurePVFItemInfoDAT(pvfPath, configDir string) (string, error) {
	exportMu.Lock()
	defer exportMu.Unlock()
	if strings.TrimSpace(pvfPath) == "" {
		return "", fmt.Errorf("pvf path is empty")
	}
	if strings.TrimSpace(configDir) == "" {
		return "", fmt.Errorf("config dir is empty")
	}
	stat, err := os.Stat(pvfPath)
	if err != nil {
		return "", err
	}
	path := filepath.Join(configDir, pvfItemInfoExportName)
	manifestPath := filepath.Join(configDir, pvfManifestName)
	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		return "", err
	}
	if pvfExportsCurrent(manifestPath, manifest, configDir) {
		return path, nil
	}
	return exportPVFItemInfoDAT(pvfPath, configDir)
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
	equipmentByID := make(map[int]shared.EquipmentCatalogItem, len(equipment))
	type row struct {
		id   int
		text string
	}
	rows := make([]row, 0, len(raw)+len(equipment)+len(stackable))
	seen := make(map[int]bool, len(raw)+len(equipment)+len(stackable))
	clientIncompatible := make(map[int]bool)
	for _, item := range equipment {
		if item.ID <= 0 {
			continue
		}
		if !shared.ClientCompatibleEquipment(item) {
			clientIncompatible[item.ID] = true
			continue
		}
		equipmentByID[item.ID] = item
		fields := generatedItemInfoFields(item, false)
		applyRawItemInfoSearchFields(fields, raw[item.ID])
		rows = append(rows, row{id: item.ID, text: strings.Join(fields, " ")})
		seen[item.ID] = true
	}
	for _, item := range stackable {
		if item.ID <= 0 {
			continue
		}
		fields := generatedItemInfoFields(item, true)
		categoryPreserved := applyRawItemInfoSearchFields(fields, raw[item.ID])
		if !categoryPreserved {
			if category := generatedRecipeItemInfoCategory(item, equipmentByID); category != 0 {
				fields[len(fields)-1] = strconv.Itoa(category)
			}
		}
		rows = append(rows, row{id: item.ID, text: strings.Join(fields, " ")})
		seen[item.ID] = true
	}
	for id, fields := range raw {
		// Do not retain a stale raw iteminfo row for equipment whose .equ
		// script is known to leave this DP2 client's ItemInfo incomplete.
		if clientIncompatible[id] {
			continue
		}
		if seen[id] {
			continue
		}
		rows = append(rows, row{id: id, text: strings.Join(fields, " ")})
		seen[id] = true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		fields := tokenizePVFItemInfo(row.text)
		if len(fields) != 17 {
			continue
		}
		fields[14] = asciiItemInfoName("item", row.id)
		fields[15] = asciiItemInfoName("name2", row.id)
		out = append(out, strings.Join(fields, " "))
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

func asciiItemInfoName(prefix string, id int) string {
	return "`" + prefix + "_" + strconv.Itoa(id) + "`"
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
		asciiItemInfoName("item", item.ID),
		asciiItemInfoName("name2", item.ID),
		strconv.Itoa(generatedItemInfoCategory(item, stackable)),
	)
	return fields
}

func generatedItemInfoJobFlags(item shared.EquipmentCatalogItem, stackable bool) []string {
	flags := make([]string, 11)
	for i := range flags {
		flags[i] = "1"
	}
	if len(item.UseJob) == 0 {
		return flags
	}
	for _, job := range item.UseJob {
		if job == 100 {
			return flags
		}
	}
	restricted := make([]string, len(flags))
	for i := range restricted {
		restricted[i] = "0"
	}
	found := false
	for _, job := range item.UseJob {
		if job >= 0 && job < len(restricted) {
			restricted[job] = "1"
			found = true
		}
	}
	if found {
		return restricted
	}
	return flags
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
	case strings.HasPrefix(path, "stackable/throw/") || slot == "throw" || slot == "set":
		return 13003
	case strings.Contains(slot, "quest"):
		return 13005
	case strings.HasPrefix(path, "stackable/professional/potion/"):
		return 33001
	case strings.HasPrefix(path, "stackable/professional/puppet/") || strings.HasPrefix(path, "stackable/professional/common/") && strings.Contains(path, "doll"):
		return 33002
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

func applyRawItemInfoSearchFields(generated, raw []string) bool {
	if len(generated) != 17 || len(raw) != 17 {
		return false
	}
	validFlags := true
	for i := 2; i <= 12; i++ {
		if raw[i] != "0" && raw[i] != "1" {
			validFlags = false
			break
		}
	}
	if validFlags {
		copy(generated[2:13], raw[2:13])
	}
	category, err := strconv.Atoi(raw[16])
	if err != nil || category <= 0 || category > 65535 {
		return false
	}
	generated[16] = raw[16]
	return true
}

func generatedRecipeItemInfoCategory(item shared.EquipmentCatalogItem, equipment map[int]shared.EquipmentCatalogItem) int {
	if item.RecipeTargetID <= 0 {
		return 0
	}
	target, ok := equipment[item.RecipeTargetID]
	if !ok {
		return 0
	}
	path := normalizePVFPath(target.Path)
	parts := strings.Split(path, "/")
	switch normalizeEquipmentTypeName(target.Slot) {
	case "weapon":
		if character := equipmentCharacterCategoryCode(parts); character > 0 {
			return 31001 + character
		}
	case "amulet", "necklace":
		return 31202
	case "wrist", "bracelet":
		return 31203
	case "ring":
		return 31204
	case "support":
		return 31302
	case "magicstone":
		return 31303
	}
	if armorClass := armorCategoryClass(parts); armorClass >= 0 {
		return 31102 + armorClass
	}
	return 31305
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
