package service

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type pvfManifest struct {
	Version int              `json:"version"`
	Source  string           `json:"source"`
	Size    int64            `json:"size"`
	ModTime int64            `json:"mod_time"`
	MD5     string           `json:"md5"`
	Runtime *runtimeManifest `json:"runtime,omitempty"`
}

const pvfExportVersion = 8

type pvfFile struct {
	Name string
	Data []byte
}

type pvfArchive struct {
	files      map[string]*pvfFile
	stringList []string
}

type equipmentCatalogItem struct {
	ID            int    `json:"id"`
	Level         int    `json:"level"`
	ItemType      int    `json:"item_type"`
	Slot          string `json:"slot,omitempty"`
	SetKey        string `json:"set_key,omitempty"`
	Rarity        int    `json:"rarity,omitempty"`
	Attach        string `json:"attach,omitempty"`
	Trade         bool   `json:"trade,omitempty"`
	NoTrade       bool   `json:"no_trade,omitempty"`
	TradeBlock    bool   `json:"trade_block,omitempty"`
	CanTrade      *bool  `json:"available_trade,omitempty"`
	CanAuction    *bool  `json:"available_auction,omitempty"`
	CanShop       *bool  `json:"available_shop,omitempty"`
	CanDrop       *bool  `json:"available_drop,omitempty"`
	Auction       bool   `json:"auction,omitempty"`
	Shop          bool   `json:"shop,omitempty"`
	BadName       bool   `json:"bad_name,omitempty"`
	NeedMaterial  bool   `json:"need_material,omitempty"`
	BasicMaterial bool   `json:"basic_material,omitempty"`
	Icon          string `json:"icon,omitempty"`
	FieldImage    string `json:"field_image,omitempty"`
	SubType       int    `json:"sub_type,omitempty"`
	Expire        bool   `json:"expire,omitempty"`
	StackLimit    int    `json:"stack_limit,omitempty"`
	UseJob        []int  `json:"use_job,omitempty"`
}

type mapCatalogItem struct {
	Village int  `json:"village"`
	Area    int  `json:"area"`
	Level   int  `json:"level"`
	XMin    int  `json:"x_min"`
	XMax    int  `json:"x_max"`
	YMin    int  `json:"y_min"`
	YMax    int  `json:"y_max"`
	Use     bool `json:"use"`
}

func ensurePVFExports(dfGameR, configDir string) error {
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
	if err := writeJSON(filepath.Join(configDir, "pvf_equipment_catalog.json"), equipment); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(configDir, "pvf_stackable_catalog.json"), stackable); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(configDir, "pvf_map_catalog.json"), maps); err != nil {
		return err
	}
	return writeJSON(manifestPath, manifest)
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
	for _, name := range []string{"pvf_equipment_catalog.json", "pvf_stackable_catalog.json", "pvf_map_catalog.json"} {
		path := filepath.Join(configDir, name)
		stat, err := os.Stat(path)
		if err != nil || stat.Size() <= 5 {
			return false
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
	for _, name := range []string{"equipment_catalog.json", "stackable_catalog.json", "map_catalog.json"} {
		_ = os.Remove(filepath.Join(configDir, name))
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
		a.stringList[i] = cleanPVFTableString(decodePVFBytes(f.Data[start+4 : end+4]))
	}
}

func extractPVFData(a *pvfArchive) ([]equipmentCatalogItem, []equipmentCatalogItem, []mapCatalogItem) {
	equipment := extractItemList(a, "equipment/equipment.lst", "equipment/", false)
	stackable := extractItemList(a, "stackable/stackable.lst", "stackable/", true)
	maps := extractMapList(a, "town/town.lst", "town/")
	return equipment, stackable, maps
}

func extractItemList(a *pvfArchive, listPath, prefix string, stackable bool) []equipmentCatalogItem {
	var out []equipmentCatalogItem
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
		item := equipmentCatalogItem{ID: entry.ID}
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
				if stackable && strings.EqualFold(cleanPVFString(nextLine(lines, i)), "ErrorString") {
					item.BadName = true
				}
			case "[rarity]":
				item.Rarity = atoi(nextLine(lines, i))
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

func deriveItemSetKey(path, body string, item equipmentCatalogItem) string {
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
		"cap", "hat", "hair", "face", "neck", "skin", "aura":
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

func extractMapList(a *pvfArchive, listPath, prefix string) []mapCatalogItem {
	var out []mapCatalogItem
	for _, entry := range parsePVFList(a.text(listPath)) {
		_, body := a.textWithExt(prefix+entry.Path, ".twn", ".map")
		if body == "" {
			continue
		}
		lines := splitPVFLines(body)
		level := 0
		for i, line := range lines {
			switch line {
			case "[name]":
			case "[limit level]":
				level = atoi(nextLine(lines, i))
			}
		}
		for _, area := range parseAreas(body) {
			out = append(out, mapCatalogItem{
				Village: entry.ID, Area: area, Level: level,
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
	return cleanPVFString(decodePVFBytes(f.Data))
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

func parseStackableTradeBlock(body string, item *equipmentCatalogItem) {
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
	switch strings.ToLower(cleanPVFString(v)) {
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
	default:
		return -1
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

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
