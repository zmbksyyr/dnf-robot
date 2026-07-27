package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const ceraItemInfoCategory = 13002

func (a *App) ensureConfiguredCeraItemInfo() ItemInfoSyncStatus {
	status := a.itemInfoStatus()
	paths := make([]string, 0, len(status.Targets)+1)
	paths = append(paths, status.SourcePath)
	paths = append(paths, status.Targets...)

	seen := make(map[string]bool, len(paths))
	var failures []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			status.Skipped++
			continue
		}
		seen[path] = true
		changed, err := a.ensureCeraItemInfoFile(path)
		if os.IsNotExist(err) {
			status.Skipped++
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		if !changed {
			status.Skipped++
			continue
		}
		status.Synced++
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusSynced, Message: path})
	}
	if len(failures) > 0 {
		status.Error = strings.Join(failures, "; ")
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
	} else if status.Synced > 0 {
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusSuccess, Message: fmt.Sprintf("synced=%d skipped=%d", status.Synced, status.Skipped)})
	}
	return status
}

func (a *App) ensureCeraItemInfoFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	updated, added, err := appendMissingCeraItemInfoRows(data, a.cfg.Cera.Items)
	if err != nil {
		return false, err
	}
	if added == 0 {
		return false, nil
	}
	if err := replaceItemInfoFile(path, updated, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func appendMissingCeraItemInfoRows(data []byte, rows []ceraRow) ([]byte, int, error) {
	ids := configuredCeraItemIDs(rows)
	missing := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		missing[id] = true
	}
	hasValidID := scanItemInfoIDs(data, func(id uint32) bool {
		delete(missing, id)
		return len(missing) == 0 && len(ids) > 0
	})
	if !hasValidID {
		return nil, 0, fmt.Errorf("iteminfo has no valid item ids")
	}

	missingIDs := make([]uint32, 0, len(missing))
	for _, id := range ids {
		if missing[id] {
			missingIDs = append(missingIDs, id)
		}
	}
	if len(missingIDs) == 0 {
		return data, 0, nil
	}

	var out bytes.Buffer
	out.Grow(len(data) + len(missingIDs)*96)
	out.Write(data)
	if len(data) > 0 {
		switch data[len(data)-1] {
		case '\n':
		case '\r':
			out.WriteByte('\n')
		default:
			out.WriteString("\r\n")
		}
	}
	for _, id := range missingIDs {
		out.WriteString(ceraItemInfoLine(id))
		out.WriteString("\r\n")
	}
	return out.Bytes(), len(missingIDs), nil
}

func scanItemInfoIDs(data []byte, visit func(uint32) bool) bool {
	found := false
	for len(data) > 0 {
		lineEnd := bytes.IndexByte(data, '\n')
		line := data
		if lineEnd >= 0 {
			line = data[:lineEnd]
			data = data[lineEnd+1:]
		} else {
			data = nil
		}
		id, ok := leadingItemInfoID(line)
		if !ok {
			continue
		}
		found = true
		if visit != nil && visit(id) {
			return true
		}
	}
	return found
}

func leadingItemInfoID(line []byte) (uint32, bool) {
	index := 0
	for index < len(line) && (line[index] == ' ' || line[index] == '\t' || line[index] == '\r') {
		index++
	}
	start := index
	var id uint64
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		id = id*10 + uint64(line[index]-'0')
		if id > uint64(^uint32(0)) {
			return 0, false
		}
		index++
	}
	if index == start || id == 0 {
		return 0, false
	}
	if index < len(line) && line[index] != ' ' && line[index] != '\t' && line[index] != '\r' {
		return 0, false
	}
	return uint32(id), true
}

func configuredCeraItemIDs(rows []ceraRow) []uint32 {
	unique := make(map[uint32]bool, len(rows))
	for _, row := range rows {
		if row.ItemID > 0 {
			unique[row.ItemID] = true
		}
	}
	ids := make([]uint32, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func ceraItemInfoLine(id uint32) string {
	fields := make([]string, 0, 17)
	fields = append(fields, strconv.FormatUint(uint64(id), 10), "2")
	for range 12 {
		fields = append(fields, "1")
	}
	itemID := strconv.FormatUint(uint64(id), 10)
	fields = append(fields, "`item_"+itemID+"`", "`name2_"+itemID+"`", strconv.Itoa(ceraItemInfoCategory))
	return strings.Join(fields, " ")
}

func replaceItemInfoFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
