package pvf

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"robot/internal/foundation/charset"
	"robot/internal/shared"
)

type pvfFile struct {
	Name string
	Data []byte
}

type pvfArchive struct {
	files      map[string]*pvfFile
	stringList []string
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

func extractPVFLevelExp(a *pvfArchive) ([]int, error) {
	if a == nil {
		return nil, fmt.Errorf("PVF archive is nil")
	}
	text := a.text("character/exptable.tbl")
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("PVF character experience table is missing")
	}
	// The PVF table starts at level 2. Keep indexes equal to character levels so
	// callers cannot accidentally shift the curve by one level.
	values := []int{0, 0}
	for _, field := range strings.Fields(text) {
		value, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	if len(values) < 3 {
		return nil, fmt.Errorf("PVF character experience table has no level values")
	}
	for level := 2; level < len(values); level++ {
		if values[level] < values[level-1] {
			return nil, fmt.Errorf("PVF character experience decreases at level %d", level)
		}
	}
	return values, nil
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
	itemSetInfo := make(map[int]pvfItemSetInfo)
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
		pathJob := -1
		if stackable {
			item.BasicMaterial = strings.HasPrefix(normalizePVFPath(entry.Path), "material/")
			parseStackableTradeBlock(body, &item)
		}
		if !stackable {
			if typ, slot := equipmentTypeFromPath(entry.Path); typ > 0 {
				item.ItemType = typ
				item.Slot = slot
			}
			pathJob = jobFromEquipmentPath(entry.Path)
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
			case "[durability]":
				item.Durability = atoi(nextLine(lines, i))
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
		if !stackable {
			applyEquipmentPathJob(&item, pathJob)
		}
		if !stackable {
			item.SetKey = deriveItemSetKey(itemPath, body, item)
		}
		if item.ID > 0 && (item.ItemType > 0 || stackable) {
			out = append(out, item)
			if !stackable {
				itemSetInfo[item.ID] = parsePVFItemSetInfo(body)
			}
		}
	}
	if !stackable {
		resolvePVFItemSetKeys(out, itemSetInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
		for _, area := range parseTownAreas(body) {
			mapBody := a.text(townMapArchivePath(area.MapPath))
			xMin, xMax, yMin, yMax, coordinateReady := townMapCoordinateBounds(mapBody)
			out = append(out, shared.MapCatalogItem{
				Village: entry.ID, VillageName: villageName, Area: area.ID, Level: level,
				XMin: xMin, XMax: xMax, YMin: yMin, YMax: yMax, Use: coordinateReady,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Village == out[j].Village {
			return out[i].Area < out[j].Area
		}
		return out[i].Village < out[j].Village
	})
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
