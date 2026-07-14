package pvf

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"robot/internal/foundation/charset"
	"robot/internal/shared"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- pvf.go ----
type pvfManifest struct {
	Version int         `json:"version"`
	Source  string      `json:"source"`
	Size    int64       `json:"size"`
	ModTime int64       `json:"mod_time"`
	MD5     string      `json:"md5"`
	Runtime interface{} `json:"runtime,omitempty"`
}

const pvfExportVersion = 1

const pvfItemInfoExportName = "pvf_iteminfo.dat"

type pvfFile struct {
	Name string
	Data []byte
}

type pvfArchive struct {
	files      map[string]*pvfFile
	stringList []string
}

func EnsureExports(dfGameR, configDir string) error {
	if dfGameR == "" || configDir == "" {
		return nil
	}
	pvfPath := filepath.Join(filepath.Dir(dfGameR), "Script.pvf")
	stat, err := os.Stat(pvfPath)
	if err != nil {
		return nil
	}
	manifest, err := buildPVFManifest(pvfPath, stat)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(configDir, "pvf_manifest.json")
	if pvfExportsCurrent(manifestPath, manifest, configDir) {
		return nil
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
	if err := writePVFItemInfoExports(configDir, archive, equipment, stackable); err != nil {
		return err
	}
	return WriteJSON(manifestPath, manifest)
}

func buildPVFManifest(path string, stat os.FileInfo) (pvfManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pvfManifest{}, err
	}
	sum := md5.Sum(data)
	return pvfManifest{
		Version: pvfExportVersion,
		Source:  path,
		Size:    stat.Size(),
		ModTime: stat.ModTime().Unix(),
		MD5:     hex.EncodeToString(sum[:]),
	}, nil
}

func pvfExportsCurrent(manifestPath string, want pvfManifest, configDir string) bool {
	for _, name := range []string{"pvf_equipment_catalog.json", "pvf_stackable_catalog.json", "pvf_map_catalog.json", pvfItemInfoExportName} {
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
	return got.Version == want.Version && got.Source == want.Source && got.Size == want.Size && got.ModTime == want.ModTime && got.MD5 == want.MD5
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

func openPVF(path string) (*pvfArchive, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 56 {
		return nil, fmt.Errorf("pvf too small")
	}
	headerLen := int(binary.LittleEndian.Uint32(raw[0:4]))
	pos := 4 + headerLen
	if pos+16 > len(raw) {
		return nil, fmt.Errorf("pvf header truncated")
	}
	treeLen := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
	treeCRC := binary.LittleEndian.Uint32(raw[pos+8 : pos+12])
	fileCount := int(binary.LittleEndian.Uint32(raw[pos+12 : pos+16]))
	treeStart := pos + 16
	treeEnd := treeStart + treeLen
	if treeEnd > len(raw) {
		return nil, fmt.Errorf("pvf file tree truncated")
	}
	tree := append([]byte(nil), raw[treeStart:treeEnd]...)
	decryptPVFBlock(tree, treeLen, treeCRC)
	dataStart := treeLen + 0x38
	if dataStart > len(raw) {
		dataStart = treeEnd
	}
	archive := &pvfArchive{files: make(map[string]*pvfFile)}
	offset := 0
	for i := 0; i < fileCount && offset+20 <= len(tree); i++ {
		nameLen := int(binary.LittleEndian.Uint32(tree[offset+4 : offset+8]))
		if nameLen < 0 || offset+20+nameLen > len(tree) {
			break
		}
		name := normalizePVFPath(string(tree[offset+8 : offset+8+nameLen]))
		fileSize := int(binary.LittleEndian.Uint32(tree[offset+8+nameLen : offset+12+nameLen]))
		fileCRC := binary.LittleEndian.Uint32(tree[offset+12+nameLen : offset+16+nameLen])
		fileOffset := int(binary.LittleEndian.Uint32(tree[offset+16+nameLen : offset+20+nameLen]))
		offset += nameLen + 20
		if name == "" || fileSize <= 0 {
			continue
		}
		aligned := align4(fileSize)
		start := dataStart + fileOffset
		end := start + aligned
		if start < 0 || end > len(raw) {
			continue
		}
		data := append([]byte(nil), raw[start:end]...)
		decryptPVFBlock(data, aligned, fileCRC)
		data = data[:fileSize]
		archive.files[name] = &pvfFile{Name: name, Data: data}
	}
	archive.loadStringTable()
	return archive, nil
}

func (a *pvfArchive) loadStringTable() {
	f := a.files["stringtable.bin"]
	if f == nil || len(f.Data) < 8 {
		return
	}
	count := int(binary.LittleEndian.Uint32(f.Data[0:4]))
	if count <= 0 || 4+count*4 > len(f.Data) {
		return
	}
	a.stringList = make([]string, count)
	for i := 0; i < count; i++ {
		start := int(binary.LittleEndian.Uint32(f.Data[4+i*4 : 8+i*4]))
		endOff := 8 + i*4
		if endOff > len(f.Data) {
			break
		}
		end := int(binary.LittleEndian.Uint32(f.Data[endOff : endOff+4]))
		if start < 0 || end < start || end+4 > len(f.Data) {
			continue
		}
		a.stringList[i] = cleanPVFTableString(charset.DecodePVFBytes(f.Data[start+4 : end+4]))
	}
}

func extractPVFData(a *pvfArchive) ([]shared.EquipmentCatalogItem, []shared.EquipmentCatalogItem, []shared.MapCatalogItem) {
	equipment := extractItemList(a, "equipment/equipment.lst", "equipment/", false)
	equipment = appendItemInfoCreatureArtifacts(equipment, a.text("etc/iteminfo.dat"))
	stackable := extractItemList(a, "stackable/stackable.lst", "stackable/", true)
	maps := extractMapList(a, "town/town.lst", "town/")
	return equipment, stackable, maps
}

func appendItemInfoCreatureArtifacts(equipment []shared.EquipmentCatalogItem, rawItemInfo string) []shared.EquipmentCatalogItem {
	rows := parsePVFItemInfoRows(formatPVFItemInfoDAT(rawItemInfo))
	if len(rows) == 0 {
		return equipment
	}
	seen := make(map[int]bool, len(equipment))
	for _, item := range equipment {
		if item.ID > 0 {
			seen[item.ID] = true
		}
	}
	ids := make([]int, 0)
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		fields := rows[id]
		if len(fields) < 17 {
			continue
		}
		slot, itemType, ok := creatureArtifactCategory(fields[16])
		if !ok {
			continue
		}
		equipment = append(equipment, shared.EquipmentCatalogItem{
			ID:       id,
			Name:     cleanPVFString(fields[14]),
			Name2:    cleanPVFString(fields[15]),
			Path:     "etc/iteminfo.dat",
			Level:    atoi(fields[13]),
			ItemType: itemType,
			Slot:     slot,
			Rarity:   atoi(fields[1]),
			Attach:   "trade",
			Trade:    true,
			Auction:  true,
		})
		seen[id] = true
	}
	return equipment
}

func creatureArtifactCategory(category string) (string, int, bool) {
	switch strings.TrimSpace(category) {
	case "14002":
		return "artifact red", 31, true
	case "14003":
		return "artifact blue", 32, true
	case "14004":
		return "artifact green", 33, true
	default:
		return "", 0, false
	}
}

func extractItemList(a *pvfArchive, listPath, prefix string, stackable bool) []shared.EquipmentCatalogItem {
	var out []shared.EquipmentCatalogItem
	for _, entry := range parsePVFList(a.text(listPath)) {
		exts := []string{".equ"}
		if stackable {
			exts = []string{".stk"}
		}
		itemPath, body := a.textWithExt(prefix+entry.Path, exts...)
		if body == "" {
			continue
		}
		lines := splitPVFLines(body)
		item := shared.EquipmentCatalogItem{ID: entry.ID, Path: itemPath}
		if stackable {
			item.BasicMaterial = strings.HasPrefix(normalizePVFPath(entry.Path), "material/")
			parseStackableTradeBlock(body, &item)
		}
		if !stackable {
			if typ, slot := equipmentTypeFromPath(entry.Path); typ > 0 {
				item.ItemType = typ
				item.Slot = slot
			}
			if job := jobFromEquipmentPath(entry.Path); job >= 0 {
				item.UseJob = appendUniqueInt(item.UseJob, job)
			}
		}
		for i, line := range lines {
			lowerLine := strings.ToLower(line)
			if stackable {
				switch {
				case strings.Contains(lowerLine, "not") && strings.Contains(lowerLine, "trade"):
					item.NoTrade = true
				case strings.Contains(lowerLine, "unable") && strings.Contains(lowerLine, "trade"):
					item.NoTrade = true
				case strings.Contains(lowerLine, "trade"):
					item.Trade = true
				case strings.Contains(lowerLine, "auction"):
					item.Auction = true
				case strings.Contains(lowerLine, "shop"):
					item.Shop = true
				}
			}
			switch line {
			case "[name]":
				item.Name = cleanPVFString(nextLine(lines, i))
				if stackable && strings.EqualFold(item.Name, "ErrorString") {
					item.BadName = true
				}
			case "[name2]":
				item.Name2 = cleanPVFString(nextLine(lines, i))
			case "[rarity]":
				item.Rarity = atoi(nextLine(lines, i))
			case "[price]":
				item.Price = atoi(nextLine(lines, i))
			case "[value]":
				item.Value = atoi(nextLine(lines, i))
			case "[minimum level]":
				item.Level = atoi(nextLine(lines, i))
			case "[stack limit]":
				item.StackLimit = atoi(nextLine(lines, i))
			case "[equipment type]":
				slot := cleanPVFString(nextLine(lines, i))
				if typ := equipmentType(slot); typ > 0 {
					item.Slot = slot
					item.ItemType = typ
				}
			case "[usable job]":
				for _, job := range parseJobs(section(body, "[usable job]", "[/usable job]")) {
					item.UseJob = appendUniqueInt(item.UseJob, job)
				}
			case "[attach type]":
				item.Attach = cleanPVFString(nextLine(lines, i))
			case "[icon]":
				item.Icon = cleanPVFString(nextLine(lines, i))
			case "[field image]":
				item.FieldImage = cleanPVFString(nextLine(lines, i))
			case "[need material]":
				item.NeedMaterial = true
			case "[sub type]":
				item.SubType = atoi(nextLine(lines, i))
			case "[expiration date]", "[usable period]":
				item.Expire = true
			case "[stackable type]":
				item.Slot = cleanPVFString(nextLine(lines, i))
			}
		}
		if !stackable && item.ItemType >= 20 && item.ItemType <= 29 {
			if job := jobFromEquipmentPath(entry.Path); job >= 0 {
				item.UseJob = []int{job}
			}
		}
		if !stackable {
			item.SetKey = deriveItemSetKey(itemPath, body, item)
		}
		if item.ID > 0 && (item.ItemType > 0 || stackable) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

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

func extractMapList(a *pvfArchive, listPath, prefix string) []shared.MapCatalogItem {
	var out []shared.MapCatalogItem
	for _, entry := range parsePVFList(a.text(listPath)) {
		_, body := a.textWithExt(prefix+entry.Path, ".twn", ".map")
		if body == "" {
			continue
		}
		lines := splitPVFLines(body)
		level := 0
		villageName := ""
		for i, line := range lines {
			switch line {
			case "[name]":
				villageName = cleanPVFString(nextLine(lines, i))
			case "[limit level]":
				level = atoi(nextLine(lines, i))
			}
		}
		for _, area := range parseAreas(body) {
			out = append(out, shared.MapCatalogItem{
				Village: entry.ID, VillageName: villageName, Area: area, Level: level,
				XMin: 240, XMax: 420, YMin: 180, YMax: 320, Use: true,
			})
		}
	}
	out = applyReferenceTownCoordinates(out)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Village == out[j].Village {
			return out[i].Area < out[j].Area
		}
		return out[i].Village < out[j].Village
	})
	return out
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

func (a *pvfArchive) text(path string) string {
	f := a.files[normalizePVFPath(path)]
	if f == nil {
		return ""
	}
	if len(f.Data) >= 2 && binary.LittleEndian.Uint16(f.Data[0:2]) == 0xd0b0 {
		return a.decodeScript(f.Data)
	}
	return cleanPVFString(charset.DecodePVFBytes(f.Data))
}

func (a *pvfArchive) textWithExt(path string, exts ...string) (string, string) {
	path = normalizePVFPath(path)
	if text := a.text(path); text != "" {
		return path, text
	}
	for _, ext := range exts {
		if strings.HasSuffix(path, ext) {
			continue
		}
		candidate := path + ext
		if text := a.text(candidate); text != "" {
			return candidate, text
		}
	}
	return path, ""
}

func (a *pvfArchive) decodeScript(data []byte) string {
	var b strings.Builder
	b.WriteString("#PVF_File\r\n")
	for i := 2; i+5 <= len(data); i += 5 {
		typ := data[i]
		val := int32(binary.LittleEndian.Uint32(data[i+1 : i+5]))
		switch typ {
		case 2:
			b.WriteString(strconv.Itoa(int(val)))
			b.WriteByte('\t')
		case 4:
			b.WriteString(strconv.FormatFloat(float64(math.Float32frombits(uint32(val))), 'f', 2, 32))
			b.WriteByte('\t')
		case 5:
			b.WriteString("\r\n")
			b.WriteString(a.lookupString(int(val)))
			b.WriteString("\r\n")
		case 7:
			b.WriteByte('`')
			b.WriteString(a.lookupString(int(val)))
			b.WriteString("`\t\n")
		case 10:
			b.WriteString(a.lookupString(int(val)))
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

func (a *pvfArchive) lookupString(index int) string {
	if index >= 0 && index < len(a.stringList) && a.stringList[index] != "" {
		return a.stringList[index]
	}
	return "ErrorString"
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

func align4(v int) int {
	return (v + 3) &^ 3
}

func decryptPVFBlock(buf []byte, size int, key uint32) {
	if size > len(buf) {
		size = len(buf)
	}
	size = align4(size)
	if size > len(buf) {
		size = len(buf) &^ 3
	}
	for i := 0; i+4 <= size; i += 4 {
		v := binary.LittleEndian.Uint32(buf[i:i+4]) ^ 0x81a79011 ^ key
		binary.LittleEndian.PutUint32(buf[i:i+4], (v>>6)|(v<<(32-6)))
	}
}

const upgradeSeparatePath = "etc/upgrade_separate.etc"
const upgradeSeparateLabel = "[separate upgrade max]"

type PVFUpgradeSeparateStatus struct {
	Path       string `json:"path"`
	Value      int    `json:"value"`
	OK         bool   `json:"ok"`
	NeedsPatch bool   `json:"needs_patch"`
	Message    string `json:"message,omitempty"`
}

type PVFUpgradeSeparatePatchResult struct {
	Path       string `json:"path"`
	BackupPath string `json:"backup_path,omitempty"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
	Patched    bool   `json:"patched"`
	OK         bool   `json:"ok"`
	Message    string `json:"message,omitempty"`
}

type pvfPatchEntry struct {
	treeOffset       int
	fileNameChecksum uint32
	name             string
	dataLen          uint32
	checksum         uint32
	offset           uint32
}

type pvfPatchMeta struct {
	fileCount          uint32
	treeChecksumOffset int
	treeStart          int
	treeEnd            int
	dataStart          int
}

func InspectPVFUpgradeSeparate(path string) (PVFUpgradeSeparateStatus, error) {
	path = cleanPVFFilePath(path)
	status := PVFUpgradeSeparateStatus{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return status, err
	}
	data, strings, _, _, _, err := readPVFPlainFile(raw, upgradeSeparatePath)
	if err != nil {
		return status, err
	}
	value, _, err := findUpgradeSeparateValue(data, strings)
	if err != nil {
		return status, err
	}
	status.Value = value
	status.OK = value <= 7
	status.NeedsPatch = value > 7
	if status.OK {
		status.Message = "upgrade separate max is compatible"
	} else {
		status.Message = "upgrade separate max is above 7"
	}
	return status, nil
}

func PatchPVFUpgradeSeparate(path string, target int) (PVFUpgradeSeparatePatchResult, error) {
	path = cleanPVFFilePath(path)
	if target <= 0 {
		target = 7
	}
	result := PVFUpgradeSeparatePatchResult{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	data, strings, entry, tree, meta, err := readPVFPlainFile(raw, upgradeSeparatePath)
	if err != nil {
		return result, err
	}
	before, valueOffset, err := findUpgradeSeparateValue(data, strings)
	if err != nil {
		return result, err
	}
	result.Before = before
	result.After = before
	result.OK = before <= target

	backupPath := fmt.Sprintf("%s.bak_upgrade_separate_%s", path, time.Now().Format("20060102_150405"))
	if err := copyFile(path, backupPath); err != nil {
		return result, fmt.Errorf("backup pvf: %w", err)
	}
	result.BackupPath = backupPath

	if before <= target {
		result.Message = "no patch needed"
		return result, nil
	}

	binary.LittleEndian.PutUint32(data[valueOffset:valueOffset+4], uint32(target))
	alignedLen := align4(int(entry.dataLen))
	aligned := make([]byte, alignedLen)
	copy(aligned, data[:entry.dataLen])
	newChecksum := pvfDataChecksum(aligned, alignedLen, entry.fileNameChecksum)

	checksumOffset := entry.treeOffset + 12 + len(entry.name)
	binary.LittleEndian.PutUint32(tree[checksumOffset:checksumOffset+4], newChecksum)

	newTreeChecksum := pvfDataChecksum(tree, len(tree), meta.fileCount)
	binary.LittleEndian.PutUint32(raw[meta.treeChecksumOffset:meta.treeChecksumOffset+4], newTreeChecksum)
	encryptPVFBlockInto(raw[meta.treeStart:meta.treeEnd], tree, newTreeChecksum)

	encryptPVFBlockInto(raw[meta.dataStart+int(entry.offset):meta.dataStart+int(entry.offset)+alignedLen], aligned, newChecksum)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		return result, err
	}
	result.After = target
	result.Patched = true
	result.OK = true
	result.Message = "patched upgrade separate max"
	return result, nil
}

func readPVFPlainFile(raw []byte, target string) ([]byte, []string, pvfPatchEntry, []byte, pvfPatchMeta, error) {
	if len(raw) < 56 {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf too small")
	}
	guidLen := int(binary.LittleEndian.Uint32(raw[0:4]))
	pos := 4 + guidLen
	if pos+16 > len(raw) {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf header truncated")
	}
	treeLen := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
	treeChecksum := binary.LittleEndian.Uint32(raw[pos+8 : pos+12])
	fileCount := binary.LittleEndian.Uint32(raw[pos+12 : pos+16])
	treeStart := pos + 16
	treeEnd := treeStart + treeLen
	if treeEnd > len(raw) {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf file tree truncated")
	}
	tree := append([]byte(nil), raw[treeStart:treeEnd]...)
	decryptPVFBlock(tree, treeLen, treeChecksum)
	meta := pvfPatchMeta{
		fileCount:          fileCount,
		treeChecksumOffset: pos + 8,
		treeStart:          treeStart,
		treeEnd:            treeEnd,
		dataStart:          treeEnd,
	}
	entries := parsePVFPatchEntries(tree, int(fileCount))
	var targetEntry *pvfPatchEntry
	var stringEntry *pvfPatchEntry
	for i := range entries {
		if entries[i].name == target {
			targetEntry = &entries[i]
		}
		if entries[i].name == "stringtable.bin" {
			stringEntry = &entries[i]
		}
	}
	if targetEntry == nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, fmt.Errorf("%s not found", target)
	}
	if stringEntry == nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, fmt.Errorf("stringtable.bin not found")
	}
	strings, err := readPVFStringTable(raw, meta.dataStart, *stringEntry)
	if err != nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, err
	}
	data, err := readPVFEntryData(raw, meta.dataStart, *targetEntry)
	if err != nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, err
	}
	return data, strings, *targetEntry, tree, meta, nil
}

func parsePVFPatchEntries(tree []byte, fileCount int) []pvfPatchEntry {
	entries := make([]pvfPatchEntry, 0, fileCount)
	for offset, i := 0, 0; i < fileCount && offset+20 <= len(tree); i++ {
		nameLen := int(binary.LittleEndian.Uint32(tree[offset+4 : offset+8]))
		if nameLen < 0 || offset+20+nameLen > len(tree) {
			break
		}
		name := normalizePVFPath(string(tree[offset+8 : offset+8+nameLen]))
		entry := pvfPatchEntry{
			treeOffset:       offset,
			fileNameChecksum: binary.LittleEndian.Uint32(tree[offset : offset+4]),
			name:             name,
			dataLen:          binary.LittleEndian.Uint32(tree[offset+8+nameLen : offset+12+nameLen]),
			checksum:         binary.LittleEndian.Uint32(tree[offset+12+nameLen : offset+16+nameLen]),
			offset:           binary.LittleEndian.Uint32(tree[offset+16+nameLen : offset+20+nameLen]),
		}
		entries = append(entries, entry)
		offset += nameLen + 20
	}
	return entries
}

func readPVFEntryData(raw []byte, dataStart int, entry pvfPatchEntry) ([]byte, error) {
	aligned := align4(int(entry.dataLen))
	start := dataStart + int(entry.offset)
	end := start + aligned
	if start < 0 || end > len(raw) {
		return nil, fmt.Errorf("pvf data for %s truncated", entry.name)
	}
	data := append([]byte(nil), raw[start:end]...)
	decryptPVFBlock(data, aligned, entry.checksum)
	return data[:entry.dataLen], nil
}

func readPVFStringTable(raw []byte, dataStart int, entry pvfPatchEntry) ([]string, error) {
	data, err := readPVFEntryData(raw, dataStart, entry)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("stringtable.bin too small")
	}
	count := int(binary.LittleEndian.Uint32(data[0:4]))
	if count <= 0 || 4+count*4 > len(data) {
		return nil, fmt.Errorf("stringtable.bin invalid count")
	}
	out := make([]string, count)
	for i := 0; i < count; i++ {
		start := int(binary.LittleEndian.Uint32(data[4+i*4 : 8+i*4]))
		endOff := 8 + i*4
		if endOff+4 > len(data) {
			break
		}
		end := int(binary.LittleEndian.Uint32(data[endOff : endOff+4]))
		if start < 0 || end < start || end+4 > len(data) {
			continue
		}
		out[i] = cleanPVFTableString(charset.DecodePVFBytes(data[start+4 : end+4]))
	}
	return out, nil
}

func findUpgradeSeparateValue(data []byte, strings []string) (int, int, error) {
	for i := 2; i+10 <= len(data); i += 5 {
		if data[i] != 5 {
			continue
		}
		idx := int(binary.LittleEndian.Uint32(data[i+1 : i+5]))
		if idx < 0 || idx >= len(strings) || strings[idx] != upgradeSeparateLabel {
			continue
		}
		if data[i+5] != 2 {
			return 0, 0, fmt.Errorf("%s value has unexpected type %d", upgradeSeparateLabel, data[i+5])
		}
		return int(binary.LittleEndian.Uint32(data[i+6 : i+10])), i + 6, nil
	}
	return 0, 0, fmt.Errorf("%s value not found", upgradeSeparateLabel)
}

var pvfChecksumTable [256]uint32
var pvfChecksumReady bool

func pvfDataChecksum(buf []byte, dataLen int, seed uint32) uint32 {
	initPVFChecksumTable()
	if dataLen > len(buf) {
		dataLen = len(buf)
	}
	num := ^seed
	for index := 0; index+3 < dataLen; index += 4 {
		b0 := (buf[index] ^ byte(num)) & 0xff
		num3 := (num >> 8) ^ pvfChecksumTable[b0]
		b1 := (buf[index+1] ^ byte(num3)) & 0xff
		num5 := (num3 >> 8) ^ pvfChecksumTable[b1]
		b2 := (buf[index+2] ^ byte(num5)) & 0xff
		num7 := (num5 >> 8) ^ pvfChecksumTable[b2]
		b3 := (buf[index+3] ^ byte(num7)) & 0xff
		num = (num7 >> 8) ^ pvfChecksumTable[b3]
	}
	return ^num
}

func initPVFChecksumTable() {
	if pvfChecksumReady {
		return
	}
	current := uint32(1)
	for step := uint32(128); step > 0; step /= 2 {
		poly := uint32(0)
		if current&1 != 0 {
			poly = 0xEDB88320
		}
		current = (current >> 1) ^ poly
		for index, currentPos := uint32(0), step; index < 256; index += step * 2 {
			pvfChecksumTable[currentPos] = pvfChecksumTable[index] ^ current
			currentPos += step * 2
		}
	}
	pvfChecksumReady = true
}

func encryptPVFBlockInto(dst []byte, plain []byte, key uint32) {
	for i := 0; i+4 <= len(dst) && i+4 <= len(plain); i += 4 {
		v := binary.LittleEndian.Uint32(plain[i : i+4])
		v = (v << 6) | (v >> (32 - 6))
		binary.LittleEndian.PutUint32(dst[i:i+4], v^key^0x81A79011)
	}
}

func cleanPVFFilePath(path string) string {
	if path == "" {
		return path
	}
	return filepath.Clean(path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func WriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// ---- map_reference.go ----
var referenceTownMaps = []shared.MapCatalogItem{
	{Village: 1, Area: 0, Level: 1, XMin: 26, XMax: 1497, YMin: 196, YMax: 337, Use: true},
	{Village: 1, Area: 2, Level: 1, XMin: 26, XMax: 832, YMin: 192, YMax: 319, Use: true},
	{Village: 2, Area: 0, Level: 3, XMin: 26, XMax: 3451, YMin: 226, YMax: 445, Use: true},
	{Village: 2, Area: 1, Level: 3, XMin: 116, XMax: 3638, YMin: 223, YMax: 330, Use: true},
	{Village: 2, Area: 2, Level: 3, XMin: 106, XMax: 2283, YMin: 221, YMax: 336, Use: true},
	{Village: 2, Area: 3, Level: 3, XMin: 89, XMax: 962, YMin: 221, YMax: 336, Use: true},
	{Village: 2, Area: 4, Level: 3, XMin: 132, XMax: 1200, YMin: 172, YMax: 322, Use: true},
	{Village: 2, Area: 6, Level: 3, XMin: 130, XMax: 715, YMin: 144, YMax: 301, Use: true},
	{Village: 2, Area: 7, Level: 3, XMin: 74, XMax: 799, YMin: 217, YMax: 340, Use: true},
	{Village: 2, Area: 8, Level: 3, XMin: 74, XMax: 799, YMin: 217, YMax: 340, Use: true},
	{Village: 2, Area: 9, Level: 3, XMin: 107, XMax: 748, YMin: 210, YMax: 348, Use: true},
	{Village: 3, Area: 0, Level: 13, XMin: 107, XMax: 2783, YMin: 226, YMax: 452, Use: true},
	{Village: 3, Area: 1, Level: 13, XMin: 60, XMax: 1691, YMin: 222, YMax: 329, Use: true},
	{Village: 3, Area: 3, Level: 13, XMin: 60, XMax: 761, YMin: 114, YMax: 301, Use: true},
	{Village: 3, Area: 4, Level: 13, XMin: 60, XMax: 741, YMin: 234, YMax: 335, Use: true},
	{Village: 3, Area: 5, Level: 13, XMin: 60, XMax: 760, YMin: 114, YMax: 301, Use: true},
	{Village: 3, Area: 6, Level: 13, XMin: 60, XMax: 786, YMin: 239, YMax: 350, Use: true},
	{Village: 3, Area: 7, Level: 13, XMin: 70, XMax: 815, YMin: 232, YMax: 353, Use: true},
	{Village: 3, Area: 8, Level: 13, XMin: 70, XMax: 759, YMin: 209, YMax: 337, Use: true},
	{Village: 3, Area: 9, Level: 13, XMin: 105, XMax: 768, YMin: 221, YMax: 344, Use: true},
	{Village: 3, Area: 10, Level: 13, XMin: 95, XMax: 787, YMin: 207, YMax: 345, Use: true},
	{Village: 4, Area: 0, Level: 37, XMin: 72, XMax: 2332, YMin: 184, YMax: 441, Use: true},
	{Village: 4, Area: 2, Level: 37, XMin: 72, XMax: 899, YMin: 173, YMax: 322, Use: true},
	{Village: 4, Area: 3, Level: 37, XMin: 17, XMax: 912, YMin: 183, YMax: 319, Use: true},
	{Village: 4, Area: 4, Level: 37, XMin: 207, XMax: 1011, YMin: 171, YMax: 320, Use: true},
	{Village: 4, Area: 5, Level: 37, XMin: 106, XMax: 811, YMin: 193, YMax: 320, Use: true},
	{Village: 5, Area: 0, Level: 42, XMin: 112, XMax: 2111, YMin: 400, YMax: 900, Use: true},
	{Village: 5, Area: 2, Level: 42, XMin: 80, XMax: 799, YMin: 353, YMax: 582, Use: true},
	{Village: 5, Area: 3, Level: 42, XMin: 95, XMax: 786, YMin: 353, YMax: 582, Use: true},
	{Village: 6, Area: 0, Level: 55, XMin: 122, XMax: 2560, YMin: 211, YMax: 343, Use: true},
	{Village: 6, Area: 1, Level: 55, XMin: 40, XMax: 1839, YMin: 210, YMax: 330, Use: true},
	{Village: 6, Area: 2, Level: 55, XMin: 195, XMax: 831, YMin: 139, YMax: 269, Use: true},
	{Village: 6, Area: 3, Level: 55, XMin: 195, XMax: 831, YMin: 139, YMax: 269, Use: true},
	{Village: 6, Area: 5, Level: 55, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 0, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 1, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 2, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 3, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 4, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 5, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 0, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 1, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 2, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 3, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 9, Area: 0, Level: 65, XMin: 91, XMax: 1458, YMin: 205, YMax: 333, Use: true},
	{Village: 9, Area: 1, Level: 65, XMin: 20, XMax: 772, YMin: 143, YMax: 315, Use: true},
	{Village: 9, Area: 3, Level: 65, XMin: 38, XMax: 813, YMin: 205, YMax: 333, Use: true},
	{Village: 10, Area: 1, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 11, Area: 0, Level: 70, XMin: 67, XMax: 2580, YMin: 219, YMax: 333, Use: true},
	{Village: 11, Area: 1, Level: 70, XMin: 100, XMax: 1473, YMin: 206, YMax: 339, Use: true},
	{Village: 11, Area: 2, Level: 70, XMin: 67, XMax: 942, YMin: 230, YMax: 350, Use: true},
	{Village: 12, Area: 0, Level: 70, XMin: 95, XMax: 720, YMin: 206, YMax: 353, Use: true},
	{Village: 13, Area: 0, Level: 70, XMin: 116, XMax: 780, YMin: 206, YMax: 353, Use: true},
	{Village: 14, Area: 0, Level: 78, XMin: 85, XMax: 1407, YMin: 212, YMax: 341, Use: true},
	{Village: 14, Area: 1, Level: 78, XMin: 46, XMax: 1910, YMin: 216, YMax: 290, Use: true},
	{Village: 14, Area: 2, Level: 78, XMin: 37, XMax: 786, YMin: 200, YMax: 340, Use: true},
}

func applyReferenceTownCoordinates(items []shared.MapCatalogItem) []shared.MapCatalogItem {
	byKey := make(map[[2]int]int, len(items))
	for i := range items {
		byKey[[2]int{items[i].Village, items[i].Area}] = i
	}
	for _, ref := range referenceTownMaps {
		key := [2]int{ref.Village, ref.Area}
		if idx, ok := byKey[key]; ok {
			ref.VillageName = items[idx].VillageName
			items[idx] = ref
			continue
		}
		byKey[key] = len(items)
		items = append(items, ref)
	}
	return items
}
