package store

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxStoreTitleBytes         = 48
	recommendedStoreTitleCount = 64
)

type TitleCatalog struct {
	titles []string
}

func LoadTitleCatalog(path string) (*TitleCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &TitleCatalog{}, err
	}
	var titles []string
	if err := json.Unmarshal(data, &titles); err != nil {
		return &TitleCatalog{}, err
	}
	return NewTitleCatalog(titles), nil
}

func NewTitleCatalog(titles []string) *TitleCatalog {
	out := &TitleCatalog{titles: make([]string, 0, len(titles))}
	seen := make(map[string]struct{}, len(titles))
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if !validStoreTitle(title) {
			continue
		}
		if _, exists := seen[title]; exists {
			continue
		}
		seen[title] = struct{}{}
		out.titles = append(out.titles, title)
	}
	return out
}

func (c *TitleCatalog) TitleForUID(uid int) string {
	if uid <= 0 {
		return fallbackStoreTitle(uid)
	}
	var titles []string
	if c != nil {
		titles = c.titles
	}
	bestTitle := ""
	var bestScore uint64
	found := false
	useFallback := false
	for _, title := range titles {
		score := storeTitleScore(uid, title)
		if !found || score > bestScore {
			bestTitle = title
			bestScore = score
			found = true
			useFallback = false
		}
	}
	missing := recommendedStoreTitleCount - len(titles)
	for index := 0; index < missing; index++ {
		candidate := "\x00fallback:" + strconv.Itoa(index)
		score := storeTitleScore(uid, candidate)
		if !found || score > bestScore {
			bestScore = score
			found = true
			useFallback = true
		}
	}
	if !found || useFallback || bestTitle == "" {
		return fallbackStoreTitle(uid)
	}
	return bestTitle
}

func (c *TitleCatalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.titles)
}

func validStoreTitle(title string) bool {
	return title != "" && utf8.ValidString(title) && len([]byte(title)) <= maxStoreTitleBytes && strings.IndexFunc(title, unicode.IsControl) < 0
}

func storeTitleScore(uid int, title string) uint64 {
	h := fnv.New64a()
	uidBytes := strconv.AppendInt(nil, int64(uid), 10)
	_, _ = h.Write(uidBytes)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(title))
	return h.Sum64()
}

func fallbackStoreTitle(uid int) string {
	return fmt.Sprintf("tw-%d", uid%100000)
}
