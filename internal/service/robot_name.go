package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (m *RobotManager) uniqueName(uid int, used map[string]struct{}) string {
	tpl := m.loadNameTemplates()
	for i := 0; i < 1000; i++ {
		name := m.renderName(tpl, uid, i)
		if name == "" {
			continue
		}
		if !robotNameFitsGameSlot(name) {
			continue
		}
		var x int
		dbName := robotDBName(name)
		if _, ok := used[dbName]; ok {
			continue
		}
		_ = m.db.QueryRow("SELECT 1 FROM taiwan_cain.charac_info WHERE charac_name=? LIMIT 1", dbName).Scan(&x)
		if x == 0 {
			used[dbName] = struct{}{}
			return name
		}
	}
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("Robot%d%03d", uid%100000, i)
		dbName := robotDBName(name)
		if _, ok := used[dbName]; ok {
			continue
		}
		var x int
		_ = m.db.QueryRow("SELECT 1 FROM taiwan_cain.charac_info WHERE charac_name=? LIMIT 1", dbName).Scan(&x)
		if x == 0 {
			used[dbName] = struct{}{}
			return name
		}
	}
	return fmt.Sprintf("Robot%d", time.Now().UnixNano()%1000000)
}

func (m *RobotManager) robotName(uid int, used map[string]struct{}, rc robotRuntimeConfig) string {
	if rc.NameASCIIFallback {
		prefix := rc.NameASCIIPrefix
		if prefix == "" {
			prefix = "twbot"
		}
		for attempt := 0; attempt < 200; attempt++ {
			name := fmt.Sprintf("%s%05d", prefix, (uid+attempt)%100000)
			dbName := robotDBName(name)
			if _, ok := used[dbName]; ok {
				continue
			}
			var x int
			_ = m.db.QueryRow("SELECT 1 FROM taiwan_cain.charac_info WHERE charac_name=? LIMIT 1", dbName).Scan(&x)
			if x == 1 {
				continue
			}
			used[dbName] = struct{}{}
			return name
		}
	}
	return m.uniqueName(uid, used)
}

func (m *RobotManager) renderName(t nameTemplates, uid, attempt int) string {
	if len(t.Names) > 0 {
		name := strings.TrimSpace(t.Names[m.randIntn(len(t.Names))])
		if attempt >= len(t.Names)*2 {
			name += fmt.Sprintf("%05d", uid%100000)
		}
		return name
	}
	prefix := m.randomString(t.Prefixes, "Bot")
	middle := m.randomString(t.Middles, "Name")
	suffix := m.randomString(t.Suffixes, "X")
	if t.Pattern == "" {
		t.Pattern = "{prefix}{middle}{suffix}{number}"
	}
	if t.NumberMax < t.NumberMin {
		t.NumberMin, t.NumberMax = t.NumberMax, t.NumberMin
	}
	if t.NumberMin == 0 && t.NumberMax == 0 {
		t.NumberMin, t.NumberMax = 10, 99
	}
	number := m.randBetween(t.NumberMin, t.NumberMax)
	name := strings.ReplaceAll(t.Pattern, "{prefix}", prefix)
	name = strings.ReplaceAll(name, "{middle}", middle)
	name = strings.ReplaceAll(name, "{suffix}", suffix)
	name = strings.ReplaceAll(name, "{number}", strconv.Itoa(number))
	name = strings.ReplaceAll(name, "{uid}", strconv.Itoa(uid))
	name = strings.ReplaceAll(name, "{uid_tail}", fmt.Sprintf("%05d", uid%100000))
	name = strings.ReplaceAll(name, "{attempt}", strconv.Itoa(attempt))
	if !strings.Contains(t.Pattern, "{uid}") && !strings.Contains(t.Pattern, "{uid_tail}") {
		name += fmt.Sprintf("%05d", uid%100000)
	}
	return name
}

func robotDBName(name string) string {
	return robotNameForEncoding(name, "utf8_cp1252").(string)
}

func robotNameFitsGameSlot(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len([]byte(name)) > 20 {
		return false
	}
	return string(windows1252StringBytes(utf8AsWindows1252String(name))) == name
}

func robotNameForEncoding(name, encoding string) interface{} {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "utf8_cp1252":
		return utf8AsWindows1252String(name)
	default:
		return name
	}
}
