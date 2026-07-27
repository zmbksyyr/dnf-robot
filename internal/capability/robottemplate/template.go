package robottemplate

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/charset"
)

type ShoutTemplates struct {
	Channel  string   `json:"channel"`
	Type     int      `json:"type"`
	Messages []string `json:"messages"`
}

type NameTemplates struct {
	Names     []string `json:"names"`
	Prefixes  []string `json:"prefixes"`
	Middles   []string `json:"middles"`
	Suffixes  []string `json:"suffixes"`
	Pattern   string   `json:"pattern"`
	NumberMin int      `json:"number_min"`
	NumberMax int      `json:"number_max"`
}

func CloneShoutTemplates(t ShoutTemplates) ShoutTemplates {
	t.Messages = append([]string(nil), t.Messages...)
	return t
}

func SafeShoutMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "hello"
	}
	const maxBytes = 72
	var b strings.Builder
	for _, r := range msg {
		if r < 0x20 {
			continue
		}
		next := string(r)
		if b.Len()+len(next) > maxBytes {
			break
		}
		b.WriteString(next)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "hello"
	}
	return out
}

func PrepareShout(msg string, world bool) (int, string, string) {
	if world {
		return 11, "world", msg
	}
	return 3, "local", msg
}

func ParseStringListJSON(data []byte) []string {
	var list []string
	if json.Unmarshal(data, &list) == nil {
		return DedupeStrings(list)
	}
	var obj struct {
		Names    []string `json:"names"`
		Messages []string `json:"messages"`
	}
	if json.Unmarshal(data, &obj) == nil {
		if len(obj.Names) > 0 {
			return DedupeStrings(obj.Names)
		}
		return DedupeStrings(obj.Messages)
	}
	return nil
}

func DedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func RenderName(t NameTemplates, uid, attempt int, randomString func([]string, string) string, randBetween func(int, int) int) string {
	if len(t.Names) > 0 {
		idx := 0
		if randBetween != nil {
			idx = randBetween(0, len(t.Names)-1)
		}
		name := strings.TrimSpace(t.Names[idx])
		if attempt >= len(t.Names)*2 {
			name += fmt.Sprintf("%05d", uid%100000)
		}
		return name
	}
	if randomString == nil {
		randomString = firstString
	}
	if randBetween == nil {
		randBetween = firstInt
	}
	prefix := randomString(t.Prefixes, "Bot")
	middle := randomString(t.Middles, "Name")
	suffix := randomString(t.Suffixes, "X")
	if t.Pattern == "" {
		t.Pattern = "{prefix}{middle}{suffix}{number}"
	}
	if t.NumberMax < t.NumberMin {
		t.NumberMin, t.NumberMax = t.NumberMax, t.NumberMin
	}
	if t.NumberMin == 0 && t.NumberMax == 0 {
		t.NumberMin, t.NumberMax = 10, 99
	}
	number := randBetween(t.NumberMin, t.NumberMax)
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

func AllocateName(uid int, used map[string]struct{}, rc robotconfig.RuntimeConfig, tpl NameTemplates, exists func(string) bool, randomString func([]string, string) string, randBetween func(int, int) int) string {
	if used == nil {
		used = make(map[string]struct{})
	}
	if rc.NameASCIIFallback {
		prefix := rc.NameASCIIPrefix
		if prefix == "" {
			prefix = "twbot"
		}
		for attempt := 0; attempt < 200; attempt++ {
			name := fmt.Sprintf("%s%05d", prefix, (uid+attempt)%100000)
			if reserveName(name, used, exists) {
				return name
			}
		}
	}
	for attempt := 0; attempt < 1000; attempt++ {
		name := RenderName(tpl, uid, attempt, randomString, randBetween)
		if !FitsGameSlot(name) {
			continue
		}
		if reserveName(name, used, exists) {
			return name
		}
	}
	for attempt := 0; attempt < 1000; attempt++ {
		name := fmt.Sprintf("Robot%d%03d", uid%100000, attempt)
		if reserveName(name, used, exists) {
			return name
		}
	}
	return fmt.Sprintf("Robot%d", time.Now().UnixNano()%1000000)
}

func reserveName(name string, used map[string]struct{}, exists func(string) bool) bool {
	dbName := DBName(name)
	if _, ok := used[dbName]; ok {
		return false
	}
	if exists != nil && exists(dbName) {
		return false
	}
	used[dbName] = struct{}{}
	return true
}

func DBName(name string) string {
	return NameForEncoding(name, "utf8_cp1252").(string)
}

func FitsGameSlot(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len([]byte(name)) > 20 {
		return false
	}
	return string(charset.Windows1252StringBytes(charset.UTF8AsWindows1252String(name))) == name
}

func NameForEncoding(name, encoding string) interface{} {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "utf8_cp1252":
		return charset.UTF8AsWindows1252String(name)
	default:
		return name
	}
}

func firstString(vals []string, fallback string) string {
	if len(vals) == 0 {
		return fallback
	}
	return vals[0]
}

func firstInt(min, max int) int {
	return min
}
