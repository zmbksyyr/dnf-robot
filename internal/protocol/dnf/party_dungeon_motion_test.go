package dnf

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestFollowExtractsPlayerWithoutReplayingControlledMonsters(t *testing.T) {
	// Combine separately observed monster and player records. The monster
	// appears first and its controller is the leader; it must not be copied
	// into the Robot's outgoing motion or treated as the player.
	monster := motionCaptureBody(t, "011102fc8d110116000000107f514100c04d440080954324faffff")
	player := motionCaptureBody(t, "011101200011012000020030555541000077440080aa43")
	m, ok := decodePartyDungeonMotion(monster)
	if !ok {
		t.Fatal("monster fixture")
	}
	p, ok := decodePartyDungeonMotion(player)
	if !ok {
		t.Fatal("player fixture")
	}
	m[0].controller = p[0].entity
	body, ok := buildPartyDungeonMotion(append(m, p...))
	if !ok {
		t.Fatal("combined motion")
	}
	original := append([]byte(nil), body...)
	follow, command, ok := rewritePartyDungeonBody(body, 32, 34)
	if !ok || command != partyDungeonStateCommand {
		t.Fatal("leader motion was not selected")
	}
	got, ok := decodePartyDungeonMotion(follow)
	if !ok || len(got) != 1 || got[0].entity != partyDungeonPlayerReference(34) || got[0].controller != got[0].entity || got[0].values != p[0].values || got[0].modes != p[0].modes {
		t.Fatalf("follow changed motion or replayed a monster: %+v", got)
	}
	if !bytes.Equal(original, body) || binary.LittleEndian.Uint16(body[1:3]) != 4 {
		t.Fatal("input motion mutated")
	}
	if _, _, ok := rewritePartyDungeonBody(monster, 22, 34); ok {
		t.Fatal("controlled monster treated as player")
	}
}

func motionCaptureBody(t *testing.T, payload string) []byte {
	t.Helper()
	plain := mustPartyHex(t, payload)
	body := make([]byte, 7+len(plain))
	body[0], body[1] = 1, 4
	checksum := partyPayloadChecksum(plain)
	copy(body[3:7], checksum[:])
	for i, value := range plain {
		body[7+i] = value ^ 0x7e
	}
	return body
}

func TestDungeonMotionFullAndDeltaFromRealClient(t *testing.T) {
	// Same monster: full ground position, airborne motion, then x/velocity
	// delta. The y coordinate must survive the delta; velocity is not y.
	var position partyDungeonPosition
	for _, tc := range []struct {
		payload string
		x, y    float32
	}{
		{"011102fc8d110116000000107f514100c04d440080954324faffff", 823, 299},
		{"011102fc8d110116000000107f0140d08d3f440000ad43a57051c2010030420100c0c024faffff", math.Float32frombits(0x443f8dd0), 346},
		{"011102fc8d110116000000006a4962ce5c394416ae7bc2", math.Float32frombits(0x44395cce), 346},
		{"011102fc8d110116000000006a596a", math.Float32frombits(0x44395cce), 346},
	} {
		body := motionCaptureBody(t, tc.payload)
		entries, ok := decodePartyDungeonMotion(body)
		if !ok || len(entries) != 1 {
			t.Fatalf("motion parse failed: %s", tc.payload)
		}
		position = position.apply(entries[0])
		if encoded, ok := buildPartyDungeonMotion(entries); !ok || !bytes.Equal(encoded, body) {
			t.Fatalf("motion round trip changed captured bytes: %x", encoded)
		}
		if position.known != 7 || position.x != tc.x || position.y != tc.y || position.z != 0 {
			t.Fatalf("position=%+v want x=%f y=%f", position, tc.x, tc.y)
		}
	}
}

func TestDungeonMotionMultiEntityCaptureAndMalformedPacket(t *testing.T) {
	body := motionCaptureBody(t, "0411029d4f110116000200206a4960df6a074416ae7b420100c0c01102a04f110116000200206a0940ab233e446b765643a5705142010030420100c0c01102a14f110116000200206a4960df6a344416ae7b420100c0c01102a34f110116000200206a4960bcd5f64316ae7b420100c0c0")
	entries, ok := decodePartyDungeonMotion(body)
	if !ok || len(entries) != 4 || entries[0].entity != [4]byte{0x11, 2, 0x9d, 0x4f} || entries[3].entity != [4]byte{0x11, 2, 0xa3, 0x4f} {
		t.Fatalf("multi entity parse=%+v ok=%t", entries, ok)
	}
	position := (partyDungeonPosition{}).apply(entries[1])
	if position.known != 7 || math.Abs(float64(position.x)-760.557312) > 0.001 || math.Abs(float64(position.y)-214.46257) > 0.001 {
		t.Fatalf("group ordering differs from capture: %+v", position)
	}
	for n := 0; n < len(body); n++ {
		if _, ok := decodePartyDungeonMotion(body[:n]); ok {
			t.Fatalf("accepted truncated motion at %d", n)
		}
	}
	// Recalculate outer checksums so truncation/count validation is exercised
	// independently of checksum rejection.
	for _, malformed := range []string{"021102fc8d110116000000006a596a", "011102fc8d110116000000006a4962ce5c3944", "011102fc8d110116000000006a596aff", "011102fc8d110116000000006a59420000c07f0000ad43"} {
		if _, ok := decodePartyDungeonMotion(motionCaptureBody(t, malformed)); ok {
			t.Fatalf("accepted malformed capture %s", malformed)
		}
	}
}

func TestDungeonWorldTracksDeathAndRoomBoundary(t *testing.T) {
	now := time.Now()
	monster := [4]byte{0x11, 2, 0xfc, 0x8d}
	world := partyDungeonWorld{}
	body := motionCaptureBody(t, "011102fc8d110116000000107f514100c04d440080954324faffff")
	world.observe(body, now)
	self := partyDungeonPosition{x: 700, y: 299, known: 7}
	ref, point, ok := world.nearestMonster(self)
	if !ok || ref != monster || point.x != 823 || point.y != 299 {
		t.Fatalf("target=%x position=%+v ok=%t", ref, point, ok)
	}
	// A real lethal HP notification; later unreliable movement cannot revive it.
	world.observe(mustPartyHex(t, "010c00333acde56f7ce331ef86818165767e7e7e8e008b"), now)
	hpBody := mustPartyHex(t, "010c00333acde56f7ce331ef86818165767e7e7e8e008b")
	plain := make([]byte, len(hpBody)-7)
	for i, value := range hpBody[7:] {
		plain[i] = value ^ 0x7e
	}
	copy(plain[:4], monster[:])
	checksum := partyPayloadChecksum(plain)
	copy(hpBody[3:7], checksum[:])
	for i, value := range plain {
		hpBody[7+i] = value ^ 0x7e
	}
	world.observe(hpBody, now)
	world.observe(body, now.Add(time.Millisecond))
	if _, _, ok := world.nearestMonster(self); ok {
		t.Fatal("dead monster selected after a late motion record")
	}
	actor := world.actors[monster]
	if !actor.dead || !actor.hpKnown || actor.hp >= 0 {
		t.Fatalf("lethal HP not recorded: %+v", actor)
	}
	vo := &RobotVo{partyDungeonWorld: world}
	vo.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
	if len(vo.partyDungeonWorld.actors) != 0 {
		t.Fatal("old room actors retained on new loading notification")
	}
	world = partyDungeonWorld{}
	world.observe(body, now)
	if _, _, ok := world.nearestMonster(self); !ok {
		t.Fatal("living monster was lost without a death or destroy record")
	}
	destroy := motionCaptureBody(t, "1102fc8d")
	destroy[1] = 23
	world.observe(destroy, now.Add(10*time.Second))
	world.observe(body, now.Add(11*time.Second))
	if _, _, ok := world.nearestMonster(self); ok {
		t.Fatal("destroyed monster was revived by a late motion record")
	}
	// Movement with an unknown y delta must wait for a full y update.
	world = partyDungeonWorld{}
	world.observe(motionCaptureBody(t, "011102fc8d110116000000006a4962ce5c394416ae7bc2"), now)
	if _, _, ok := world.nearestMonster(self); ok {
		t.Fatal("unknown y coordinate was invented")
	}
}
