package dnf

import (
	"encoding/binary"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
	"robot/internal/shared"
)

// Raw motion observed from the real leader. It names a map object, with the
// leader as controller, at precisely the bunker location declared in the PVF.
const bunkerMotion = "01040044e8dda07f5f7e317e6f7f7c7e7e7c4e012b3f7ebe703a7e7e263d"

func TestDungeonTargetsObservedMonsterContainerAndKeepsItsDeath(t *testing.T) {
	now := time.Unix(100, 0)
	ref := [4]byte{0x21, 0, 0x4f, 0}
	object := shared.DungeonObject{MapID: 216, ObjectID: 1015, X: 571, Y: 216}
	position := partyDungeonPosition{x: 400, y: 216, known: 7}
	for _, matching := range []bool{false, true} {
		world := partyDungeonWorld{}
		if matching {
			world.objectives = []shared.DungeonObject{object}
		}
		if _, _, found := world.nearestTarget([4]byte{}, position); found {
			t.Fatal("PVF coordinates alone created a runtime actor")
		}
		world.observe(mustPartyHex(t, bunkerMotion), now)
		target, _, found := world.nearestTarget([4]byte{}, position)
		if found != matching || matching && target != ref {
			t.Fatalf("matching=%t target=%x found=%t", matching, target, found)
		}
		if !matching {
			continue
		}
		// A normal destroy notice ends pursuit. Later position replays must not
		// resurrect this object, or select decorations that share its actor class.
		body := motionCaptureBody(t, "21004f00")
		body[0], body[1] = 2, 23
		world.observe(body, now)
		world.observe(mustPartyHex(t, bunkerMotion), now.Add(time.Second))
		if _, _, found := world.nearestTarget([4]byte{}, position); found || !world.actors[ref].dead {
			t.Fatal("destroyed objective became a target again")
		}
	}
}

func TestDungeonObjectRequiresCompleteMatchingPosition(t *testing.T) {
	object := shared.DungeonObject{MapID: 216, ObjectID: 1015, X: 571, Y: 216}
	world := partyDungeonWorld{objectives: []shared.DungeonObject{object}}
	for _, position := range []partyDungeonPosition{
		{x: 571, y: 216, known: 3}, {x: 572, y: 216, known: 7}, {x: 571, y: 215, known: 7}, {x: 571, y: 216, z: 1, known: 7},
	} {
		if world.mapObjective(position) {
			t.Fatalf("matched incomplete or different position: %+v", position)
		}
	}
}

func TestRoomNotificationSelectsCurrentPVFObjectCatalog(t *testing.T) {
	// This real tutorial packet has seed 0x49e8c3cc and PVF map index 61000.
	shared.SetDungeonObjects([]shared.DungeonObject{{MapID: 61000, ObjectID: 1015, X: 571, Y: 216}})
	t.Cleanup(func() { shared.SetDungeonObjects(nil) })
	r := &RobotVo{State: StateRun, Cipher: crypt.NewDNFCipher(), Conn: &captureSessionConn{}}
	if err := r.Cipher.Initialize(make([]byte, r.Cipher.KeySize())); err != nil {
		t.Fatal(err)
	}
	body := mustPartyHex(t, capturedRoomBody)
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, body))
	if len(r.partyDungeonWorld.objectives) != 1 || !r.partyDungeonLoading {
		t.Fatal("current room's PVF objects were not selected during loading")
	}
	binary.LittleEndian.PutUint16(body[10:12], 217)
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, body))
	if len(r.partyDungeonWorld.objectives) != 0 {
		t.Fatal("room change retained old objectives")
	}
}

func TestRoomMapIDReadsMapIndexAndRejectsClearedVariant(t *testing.T) {
	body := mustPartyHex(t, capturedRoomBody)
	if mapID, ok := roomMapID(body); !ok || mapID != 61000 {
		t.Fatalf("real room map=%d known=%t, want 61000", mapID, ok)
	}
	for _, body := range [][]byte{
		body[:12],
		{0, 1, 0, 0xcc, 0xc3, 0xe8, 0x49, 0, 0, 0, 0, 0xff},
		{0, 1, 0, 0xcc, 0xc3, 0xe8, 0x49, 0, 0, 0, 0, 0xff, 216, 0, 0, 0},
	} {
		if mapID, ok := roomMapID(body); ok {
			t.Fatalf("omitted map index became %d for %x", mapID, body)
		}
	}
}
