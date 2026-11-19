package repository

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestCharacterSlotResolvesListWithoutNarrowingCID(t *testing.T) {
	raw := make([]byte, 36*148)
	for slot := 0; slot < 7; slot++ {
		binary.LittleEndian.PutUint32(raw[slot*148:], uint32(1615+slot))
	}
	binary.LittleEndian.PutUint32(raw[7*148:], 900001)
	for cid, want := range map[int]int{1615: 0, 900001: 7} {
		got, err := characterSlot(raw, cid)
		if err != nil || got != want {
			t.Fatalf("cid=%d slot=%d err=%v, want %d", cid, got, err, want)
		}
	}
	if _, err := characterSlot(raw, 1234); err == nil {
		t.Fatal("missing character accepted")
	}
	binary.LittleEndian.PutUint32(raw[8*148:], 900001)
	if _, err := characterSlot(raw, 900001); err == nil {
		t.Fatal("duplicate character accepted")
	}
}

func TestCharacterSlotRejectsMissingAndDamagedLists(t *testing.T) {
	for _, raw := range [][]byte{nil, make([]byte, 147), make([]byte, 149), make([]byte, 37*148)} {
		if _, err := characterSlot(raw, 1615); err == nil {
			t.Fatalf("accepted invalid view of %d bytes", len(raw))
		}
	}
	raw := make([]byte, 36*148)
	binary.LittleEndian.PutUint32(raw[2*148:], 1615)
	if _, err := characterSlot(raw, 1615); err == nil {
		t.Fatal("sparse list accepted despite original game's count-based load")
	}
}

func TestRobotLocationsQueryOnlyCountsNormalRoles(t *testing.T) {
	if !strings.Contains(robotLocationsQuery, "WHERE CAST(d.function_type AS SIGNED)=0") {
		t.Fatalf("robot locations query includes active store roles: %s", robotLocationsQuery)
	}
}
