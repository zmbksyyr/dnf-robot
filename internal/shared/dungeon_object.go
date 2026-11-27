package shared

import "sync/atomic"

// DungeonObject is a special map object containing monsters in the current
// PVF. Its runtime actor reference must still come from the room's peer stream.
type DungeonObject struct {
	MapID    uint32 `json:"map_id"`
	ObjectID int    `json:"object_id"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Z        int    `json:"z"`
}

var dungeonObjectSnapshot atomic.Pointer[map[uint32][]DungeonObject]

func SetDungeonObjects(objects []DungeonObject) {
	index := make(map[uint32][]DungeonObject)
	for _, object := range objects {
		index[object.MapID] = append(index[object.MapID], object)
	}
	dungeonObjectSnapshot.Store(&index)
}

func DungeonObjectsForMap(mapID uint32) []DungeonObject {
	index := dungeonObjectSnapshot.Load()
	if index == nil {
		return nil
	}
	return append([]DungeonObject(nil), (*index)[mapID]...)
}
