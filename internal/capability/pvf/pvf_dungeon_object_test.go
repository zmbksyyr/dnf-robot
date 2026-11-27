package pvf

import (
	"path/filepath"
	"reflect"
	"testing"

	"robot/internal/shared"
)

// This is the special-object section from the observed 216.map. It mixes
// items, a bunker and barrels, including an object with two content groups.
const bunkerSpecialObjects = "222 1085 160 0 1 `[item]` 3 -1 3 -1 -1 1015 571 216 0 1 `[monster]` 2 4 1 1 -1 221 895 269 0 1 `[monster]` 4 4 3 10 -1 221 652 306 0 2 `[item]` 3 -1 1 -1 -1 `[monster]` 4 4 3 10 -1"

func TestDungeonObjectExtractionUsesMapListAndMonsterContent(t *testing.T) {
	body := "#PVF_File\n[special passive object]\n" + bunkerSpecialObjects + "\n[/special passive object]\n[map name]\nfixture"
	archive := &pvfArchive{files: map[string]*pvfFile{
		"map/map.lst":            {Data: []byte("87 `alternate/room.map` 216 `mirkwood/216.map`")},
		"map/mirkwood/216.map":   {Data: []byte(body)},
		"map/alternate/room.map": {Data: []byte("#PVF_File\n[special passive object]\n8 1 2 0 1 `[trap]` 9 1 1 1 -1 9 10 20 0 1 `[quest]` 1 -1 -1 -1 -1\n[/special passive object]\n[map name]\nfixture")},
	}}
	want := []shared.DungeonObject{
		{MapID: 216, ObjectID: 1015, X: 571, Y: 216},
		{MapID: 216, ObjectID: 221, X: 652, Y: 306},
		{MapID: 216, ObjectID: 221, X: 895, Y: 269},
	}
	if got := extractDungeonObjects(archive); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// IDs come from map.lst, not a numeric filename convention.
	archive.files["map/map.lst"].Data = []byte("87 `mirkwood/216.map`")
	got := extractDungeonObjects(archive)
	if len(got) != 3 || got[0].MapID != 87 {
		t.Fatalf("map list ID ignored: %+v", got)
	}
}

func TestDungeonObjectParserRejectsUnknownOrTruncatedContent(t *testing.T) {
	for _, section := range []string{
		bunkerSpecialObjects + " 1", "1 1 2 0 -1", "1 1 2 0 2 `[monster]` 1 2 3 4 5",
		"1 1 2 0 1 `[unknown]` 1 2 3 4 5", "1 1 2 0 1 `[monster]` 1 2 bad 4 5",
	} {
		if got, ok := parseDungeonObjects(216, section); ok || got != nil {
			t.Fatalf("accepted malformed section: %q -> %+v", section, got)
		}
	}
}

func TestDungeonObjectCatalogLoadsWithoutPVFAndPublishesImmutableSnapshot(t *testing.T) {
	t.Cleanup(func() { shared.SetDungeonObjects(nil) })
	dir := t.TempDir()
	want := shared.DungeonObject{MapID: 216, ObjectID: 1015, X: 571, Y: 216}
	if err := WriteJSON(filepath.Join(dir, pvfDungeonObjectExportName), dungeonObjectCatalog{Version: 1, Objects: []shared.DungeonObject{want}}); err != nil {
		t.Fatal(err)
	}
	for _, game := range []string{"", filepath.Join(dir, "missing", "df_game_r")} {
		shared.SetDungeonObjects(nil)
		if err := EnsureExports(game, dir); err != nil {
			t.Fatal(err)
		}
		got := shared.DungeonObjectsForMap(216)
		if !reflect.DeepEqual(got, []shared.DungeonObject{want}) {
			t.Fatalf("loaded %+v", got)
		}
		got[0].X = 0
		if shared.DungeonObjectsForMap(216)[0] != want || len(shared.DungeonObjectsForMap(217)) != 0 {
			t.Fatal("snapshot changed or leaked between rooms")
		}
	}
}
