package dnf

import (
	"encoding/binary"
	"testing"
)

// The server sends the real town entity record together with a structurally
// valid decoy candidate. Decoys carry coordinates far outside any town, so the
// parser must reject them instead of recording a bogus id and position.
func TestParseTownEntityPositionRejectsOutOfRangeDecoy(t *testing.T) {
	real := make([]byte, 16)
	binary.LittleEndian.PutUint16(real[0:2], 293)
	binary.LittleEndian.PutUint16(real[2:4], 1498)
	binary.LittleEndian.PutUint16(real[4:6], 335)
	real[6] = 5
	binary.LittleEndian.PutUint16(real[7:9], 100)

	position, ok := parseTownEntityPosition(real)
	if !ok {
		t.Fatal("real town entity record was rejected")
	}
	if position.uniqueID != 293 || position.x != 1498 || position.y != 335 {
		t.Fatalf("position = %+v", position)
	}

	// Shape taken from a captured decoy candidate body.
	decoy := make([]byte, 16)
	binary.LittleEndian.PutUint16(decoy[0:2], 9235)
	binary.LittleEndian.PutUint16(decoy[2:4], 44171)
	binary.LittleEndian.PutUint16(decoy[4:6], 35620)
	if _, ok := parseTownEntityPosition(decoy); ok {
		t.Fatal("out-of-range decoy was accepted as a town entity")
	}

	areaDecoy := make([]byte, 16)
	binary.LittleEndian.PutUint16(areaDecoy[0:2], 4711)
	areaDecoy[2], areaDecoy[3] = 1, 0
	binary.LittleEndian.PutUint16(areaDecoy[4:6], 47526)
	binary.LittleEndian.PutUint16(areaDecoy[6:8], 14311)
	if _, ok := parseTownEntityArea(areaDecoy); ok {
		t.Fatal("out-of-range area decoy was accepted")
	}
}
