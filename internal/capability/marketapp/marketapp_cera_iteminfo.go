package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ceraItemInfoCategory = 13002

func (a *App) ensureConfiguredCeraItemInfo() ItemInfoSyncStatus {
	status := a.itemInfoStatus()
	paths := make([]string, 0, len(status.Targets)+1)
	paths = append(paths, status.SourcePath)
	paths = append(paths, status.Targets...)
	donors, err := loadNativeCeraItemInfoRows(append(append([]string(nil), status.Targets...), status.SourcePath), a.cfg.Cera.Items)
	if err != nil {
		status.Error = err.Error()
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
		return status
	}

	seen := make(map[string]bool, len(paths))
	var failures []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			status.Skipped++
			continue
		}
		seen[path] = true
		changed, err := a.ensureCeraItemInfoFile(path, donors)
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

func (a *App) ensureCeraItemInfoFile(path string, donors map[uint32][]byte) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	updated, changed, err := mergeNativeCeraItemInfoRows(data, a.cfg.Cera.Items, donors)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := replaceItemInfoFile(path, updated, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func loadNativeCeraItemInfoRows(paths []string, rows []ceraRow) (map[uint32][]byte, error) {
	ids := configuredCeraItemIDs(rows)
	wanted := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	donors := make(map[uint32][]byte, len(ids))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		scanItemInfoLines(data, func(id uint32, line []byte) bool {
			if !wanted[id] || donors[id] != nil || !isNativeCeraItemInfoLine(id, line) {
				return false
			}
			donors[id] = append([]byte(nil), bytes.TrimRight(line, "\r\n")...)
			return len(donors) == len(ids)
		})
	}
	missing := make([]uint32, 0)
	for _, id := range ids {
		if donors[id] == nil {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("native cera iteminfo rows missing: %v", missing)
	}
	return donors, nil
}

func mergeNativeCeraItemInfoRows(data []byte, rows []ceraRow, donors map[uint32][]byte) ([]byte, bool, error) {
	ids := configuredCeraItemIDs(rows)
	if len(ids) == 0 {
		return data, false, nil
	}
	configured := make(map[uint32]bool, len(ids))
	current := make(map[uint32][]byte, len(ids))
	counts := make(map[uint32]int, len(ids))
	for _, id := range ids {
		configured[id] = true
		if donors[id] == nil {
			return nil, false, fmt.Errorf("native cera iteminfo row missing: %d", id)
		}
	}
	hasValidID := scanItemInfoLines(data, func(id uint32, line []byte) bool {
		if configured[id] {
			counts[id]++
			current[id] = bytes.TrimRight(line, "\r\n")
		}
		return false
	})
	if !hasValidID {
		return nil, false, fmt.Errorf("iteminfo has no valid item ids")
	}
	matched := true
	for _, id := range ids {
		if counts[id] != 1 || !bytes.Equal(current[id], donors[id]) {
			matched = false
			break
		}
	}
	if matched {
		return data, false, nil
	}

	var out bytes.Buffer
	out.Grow(len(data) + len(ids)*96)
	for len(data) > 0 {
		lineEnd := bytes.IndexByte(data, '\n')
		line := data
		if lineEnd >= 0 {
			line = data[:lineEnd+1]
			data = data[lineEnd+1:]
		} else {
			data = nil
		}
		id, ok := leadingItemInfoID(line)
		if ok && configured[id] {
			continue
		}
		out.Write(line)
	}
	if out.Len() > 0 && out.Bytes()[out.Len()-1] != '\n' {
		out.WriteString("\r\n")
	}
	for _, id := range ids {
		out.Write(donors[id])
		out.WriteString("\r\n")
	}
	return out.Bytes(), true, nil
}

func isNativeCeraItemInfoLine(id uint32, line []byte) bool {
	fields := bytes.Fields(line)
	if len(fields) < 17 || !bytes.Equal(fields[len(fields)-1], []byte(fmt.Sprint(ceraItemInfoCategory))) {
		return false
	}
	itemID := fmt.Sprint(id)
	return !bytes.Contains(line, []byte("`item_"+itemID+"`")) &&
		!bytes.Contains(line, []byte("`name2_"+itemID+"`"))
}

func scanItemInfoIDs(data []byte, visit func(uint32) bool) bool {
	return scanItemInfoLines(data, func(id uint32, _ []byte) bool { return visit != nil && visit(id) })
}

func scanItemInfoLines(data []byte, visit func(uint32, []byte) bool) bool {
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
		if visit != nil && visit(id, line) {
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
