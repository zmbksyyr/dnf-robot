package marketapp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"robot/internal/foundation/atomicfile"
)

func loadItemInfoRows(paths []string) (map[uint32][]byte, error) {
	rows := make(map[uint32][]byte)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		_, err := scanItemInfoFile(path, func(id uint32, line []byte) bool {
			if rows[id] == nil {
				rows[id] = append([]byte(nil), bytes.TrimRight(line, "\r\n")...)
			}
			return false
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read native iteminfo %s: %w", path, err)
		}
	}
	return rows, nil
}

// mergeItemInfoOverlay keeps the PVF row for duplicate IDs and restores only
// service-native IDs that are absent from the PVF export.
func mergeItemInfoOverlay(pvfData []byte, originalRows map[uint32][]byte) ([]byte, bool) {
	if len(originalRows) == 0 {
		return pvfData, false
	}
	present := make(map[uint32]bool)
	scanItemInfoIDs(pvfData, func(id uint32) bool {
		present[id] = true
		return false
	})
	missing := make([]uint32, 0)
	for id := range originalRows {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return pvfData, false
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })

	var out bytes.Buffer
	out.Grow(len(pvfData) + len(missing)*96)
	out.Write(pvfData)
	if out.Len() > 0 && out.Bytes()[out.Len()-1] != '\n' {
		out.WriteString("\r\n")
	}
	for _, id := range missing {
		out.Write(originalRows[id])
		out.WriteString("\r\n")
	}
	return out.Bytes(), true
}

func validateConfiguredCeraItemInfo(data []byte, rows []ceraRow) error {
	required := configuredCeraItemIDs(rows)
	if len(required) == 0 {
		return nil
	}
	present := make(map[uint32]bool, len(required))
	scanItemInfoIDs(data, func(id uint32) bool {
		present[id] = true
		return false
	})
	missing := make([]uint32, 0)
	for _, id := range required {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("configured cera iteminfo ids missing: %v", missing)
	}
	return nil
}

func validateConfiguredCeraItemInfoFile(path string, rows []ceraRow) error {
	required := configuredCeraItemIDs(rows)
	if len(required) == 0 {
		return nil
	}
	missing := make(map[uint32]bool, len(required))
	for _, id := range required {
		missing[id] = true
	}
	_, err := scanItemInfoFile(path, func(id uint32, _ []byte) bool {
		delete(missing, id)
		return len(missing) == 0
	})
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	ids := make([]uint32, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return fmt.Errorf("configured cera iteminfo ids missing: %v", ids)
}

func (a *App) ensureConfiguredCeraItemInfo() ItemInfoSyncStatus {
	cfg := a.configSnapshot()
	status := a.itemInfoStatus()
	if len(configuredCeraItemIDs(cfg.Cera.Items)) == 0 {
		return status
	}
	paths := append([]string(nil), status.Targets...)
	seen := make(map[string]bool, len(paths))
	validated := 0
	var failures []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			status.Skipped++
			continue
		}
		seen[path] = true
		err := validateConfiguredCeraItemInfoFile(path, cfg.Cera.Items)
		if os.IsNotExist(err) {
			status.Skipped++
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		validated++
	}
	if validated == 0 && len(failures) == 0 && strings.TrimSpace(status.SourcePath) != "" {
		err := validateConfiguredCeraItemInfoFile(status.SourcePath, cfg.Cera.Items)
		if err == nil {
			validated++
		} else if !os.IsNotExist(err) {
			failures = append(failures, fmt.Sprintf("%s: %v", status.SourcePath, err))
		}
	}
	if validated == 0 && len(failures) == 0 {
		failures = append(failures, "no readable iteminfo file")
	}
	if len(failures) > 0 {
		status.Error = strings.Join(failures, "; ")
		a.appendLog(LogEvent{Type: "iteminfo_cera", Status: marketLogStatusFailed, Message: status.Error})
	}
	return status
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

func scanItemInfoFile(path string, visit func(uint32, []byte) bool) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	found := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if id, ok := leadingItemInfoID(line); ok {
				found = true
				if visit != nil && visit(id, line) {
					return true, nil
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return found, nil
			}
			return found, readErr
		}
	}
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
		if row.ItemID > 0 && row.Enabled && row.RestockQty > 0 {
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
	return atomicfile.WriteFile(path, data, mode)
}

func itemInfoFileEquals(path string, want []byte) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != int64(len(want)) {
		return false, nil
	}
	buf := make([]byte, 64*1024)
	offset := 0
	for offset < len(want) {
		n, readErr := file.Read(buf)
		if n > 0 {
			if !bytes.Equal(buf[:n], want[offset:offset+n]) {
				return false, nil
			}
			offset += n
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return false, readErr
		}
	}
	return offset == len(want), nil
}
