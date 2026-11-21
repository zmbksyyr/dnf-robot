package dnf

import "testing"

// The body below is a real room information packet captured from the start room
// of the tutorial dungeon: two ordinary monsters whose instance ids are 0x552b
// and 0x552c, each at full health.
const capturedRoomBody = "000100ccc3e84900000148ee020000000000002b5513000000010000000000010000002c" +
	"551400000001000000000000ff19f03e1b0000000000000000000000"

func TestParseRoomMonsters(t *testing.T) {
	body := mustPartyHex(t, capturedRoomBody)
	monsters := parseRoomMonsters(body)
	if len(monsters) != 2 {
		t.Fatalf("monsters = %d, want 2", len(monsters))
	}
	if monsters[0].instID != 0x552b || monsters[1].instID != 0x552c {
		t.Fatalf("instance ids = %x %x, want 552b 552c", monsters[0].instID, monsters[1].instID)
	}
	if monsters[0].mobIndex != 19 || monsters[1].mobIndex != 20 {
		t.Fatalf("data indexes = %d %d, want 19 20", monsters[0].mobIndex, monsters[1].mobIndex)
	}
	for i, monster := range monsters {
		if monster.champion {
			t.Fatalf("monster %d flagged as champion", i)
		}
	}
}

func TestParseRoomMonstersRejectsShortBody(t *testing.T) {
	if monsters := parseRoomMonsters([]byte{0, 1, 0}); monsters != nil {
		t.Fatalf("monsters = %v, want nil", monsters)
	}
	// A count that runs past the end of the body must be refused rather than
	// read out of bounds.
	body := make([]byte, 20)
	body[12] = 2
	if monsters := parseRoomMonsters(body); monsters != nil {
		t.Fatalf("monsters = %v, want nil", monsters)
	}
}