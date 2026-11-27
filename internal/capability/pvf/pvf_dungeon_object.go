package pvf

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"robot/internal/shared"
)

type dungeonObjectCatalog struct {
	Version int                    `json:"version"`
	Objects []shared.DungeonObject `json:"objects"`
}

func loadDungeonObjectCatalog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var catalog dungeonObjectCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	if catalog.Version != 1 {
		return fmt.Errorf("unsupported dungeon object catalog version %d", catalog.Version)
	}
	shared.SetDungeonObjects(catalog.Objects)
	return nil
}

func extractDungeonObjects(archive *pvfArchive) []shared.DungeonObject {
	objects := make([]shared.DungeonObject, 0)
	for _, entry := range parsePVFList(archive.text("map/map.lst")) {
		path := entry.Path
		if !strings.HasPrefix(path, "map/") {
			path = "map/" + path
		}
		body := archive.text(path)
		_, section, found := strings.Cut(body, "[special passive object]")
		if !found {
			continue
		}
		section, _, found = strings.Cut(section, "[/special passive object]")
		if !found {
			continue
		}
		if parsed, ok := parseDungeonObjects(uint32(entry.ID), section); ok {
			objects = append(objects, parsed...)
		}
	}
	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if a.MapID != b.MapID {
			return a.MapID < b.MapID
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Y < b.Y
	})
	return objects
}

// Each object has id/x/y/z/count, followed by count groups of type and five
// integer parameters. Skip item/trap/quest groups without treating their parameters
// as another object, and reject the whole section on an unknown layout.
func parseDungeonObjects(mapID uint32, section string) ([]shared.DungeonObject, bool) {
	fields := tokenizePVFItemInfo(section)
	objects := make([]shared.DungeonObject, 0)
	for len(fields) > 0 {
		if len(fields) < 5 {
			return nil, false
		}
		var header [5]int
		for i := range header {
			value, err := strconv.Atoi(fields[i])
			if err != nil {
				return nil, false
			}
			header[i] = value
		}
		fields = fields[5:]
		count := header[4]
		if header[0] < 0 || count < 0 || count > len(fields)/6 {
			return nil, false
		}
		monster := false
		for i := 0; i < count; i++ {
			kind := strings.Trim(fields[0], "`")
			if kind != "[monster]" && kind != "[item]" && kind != "[trap]" && kind != "[quest]" {
				return nil, false
			}
			var params [5]int
			for j := range params {
				value, err := strconv.Atoi(fields[j+1])
				if err != nil {
					return nil, false
				}
				params[j] = value
			}
			monster = monster || kind == "[monster]" && params[0] > 0
			fields = fields[6:]
		}
		if monster {
			objects = append(objects, shared.DungeonObject{MapID: mapID, ObjectID: header[0], X: header[1], Y: header[2], Z: header[3]})
		}
	}
	return objects, true
}
