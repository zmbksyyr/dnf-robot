package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

// These three real-client HP_DAMAGE payloads retain their combat values and
// field layout. Actor references and opaque footer values are anonymized.
var dungeonDamageCaptures = []struct {
	plain string
	hp    int32
}{
	{"110105001102010300c07744550121000e000a0019000d0000000d0000007700000000000000000000000000", 119},
	{"110105001102010300007944550121000f000a0019000e0000000e0000006900000000000000000000000000", 105},
	{"110105001102010300c01a44c80000000e0004002900ea1b0000ac0000000000000000000000000000000000", 0},
}

func dungeonDamageBody(t *testing.T, plain string) []byte {
	t.Helper()
	body := motionCaptureBody(t, plain)
	body[0], body[1] = 2, 20
	return body
}

func TestDungeonDamageUpdatesVictimAndRegroupsWithoutWaitingForDeathNotice(t *testing.T) {
	for _, reliable := range []bool{false, true} {
		t.Run(map[bool]string{false: "unreliable", true: "reliable"}[reliable], func(t *testing.T) {
			now := time.Unix(100, 0)
			world, self, leader, victim := navigationTestWorld(now)
			before := world.actors[victim].position
			world.planNavigation(self, leader, now)
			r := &RobotVo{partyDungeonWorld: world, partyDungeonEnteredAt: now,
				partySelfPeer: partyIPPeer{uniqueID: 5, slot: 1, slotKnown: true}}
			peer := partyIPPeer{uniqueID: 4, slot: 0, slotKnown: true}
			for i, capture := range dungeonDamageCaptures {
				body := dungeonDamageBody(t, capture.plain)
				var frame []byte
				if reliable {
					frame = buildPartyReliablePacket(uint32(i), 0, 0, [][]byte{body})
				} else {
					frame = buildPartyUnreliablePacket(uint32(i), 0, 0, body)
				}
				r.observePartyDungeonFrameUnsafe(frame, peer, now.Add(time.Duration(i)*time.Second))
				actor := r.partyDungeonWorld.actors[victim]
				if !actor.hpKnown || actor.hp != capture.hp || actor.dead != (capture.hp == 0) || actor.position != before {
					t.Fatalf("victim did not retain motion and peer HP: %+v", actor)
				}
				if r.partyDungeonWorld.actors[self].hpKnown || r.partyDungeonWorld.actors[self].dead {
					t.Fatal("attacker was treated as the damaged actor")
				}
			}
			position, move, active := r.partyDungeonWorld.planNavigation(self, leader, now.Add(2*time.Second))
			if !active || !move || position.x != 140 || r.partyDungeonWorld.target != [4]byte{} {
				t.Fatalf("last target's HP reached zero without regrouping: position=%+v target=%x", position, r.partyDungeonWorld.target)
			}
		})
	}
}

func TestDungeonDamageRejectsCorruptAndTruncatedMessages(t *testing.T) {
	now := time.Unix(100, 0)
	valid := dungeonDamageBody(t, dungeonDamageCaptures[0].plain)
	for length := 0; length < len(valid); length++ {
		world := partyDungeonWorld{}
		world.observe(valid[:length], now)
		if len(world.actors) != 0 {
			t.Fatalf("accepted truncated damage at %d", length)
		}
	}
	corrupt := append([]byte(nil), valid...)
	corrupt[7] ^= 1
	world := partyDungeonWorld{}
	world.observe(corrupt, now)
	if len(world.actors) != 0 {
		t.Fatal("accepted damage with invalid checksum")
	}
	// A well-formed packet for another actor class is not a monster or player.
	plain := mustPartyHex(t, dungeonDamageCaptures[0].plain)
	binary.LittleEndian.PutUint16(plain[4:6], 0xf000)
	world.observe(dungeonDamageBody(t, hex.EncodeToString(plain)), now)
	if len(world.actors) != 0 {
		t.Fatal("passive attack object was stored as a living actor")
	}
}
