package dnf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

func TestDungeonOriginalSkillDamageResults(t *testing.T) {
	type capture struct {
		packet string
		hp     int32
	}
	// The support trace is an original offline callback/encoder/receiver chain
	// (support-origin-hit-consume1). The ordinary trace is live original encoder
	// output after normal input, admission and MP spending, observed with Frida
	// (support-origin-live-entry1, run3). Neither is Robot combat acceptance.
	for _, trace := range []struct {
		name     string
		victim   [4]byte
		source   [4]byte
		captures []capture
	}{
		{"support", [4]byte{0x11, 0x02, 0xa7, 0xcd}, [4]byte{0x00, 0xf0, 0xf6, 0x2a}, []capture{
			{"010c003f02d1851102a7cd1decffff3018000000f0f62a", -5091},
			{"021400da12e3e600f0f62a1102a7cd00008b4438013d00000000000000301800004d040000000000000000000000002f5f4bc1", 0},
		}},
		{"ordinary", [4]byte{0x11, 0x02, 0xdb, 0x9a}, [4]byte{0x11, 0x01, 0x02, 0x00}, []capture{
			{"010c00f3fd2edf1102db9a1fefffff6016000011010200", -4321},
			{"02140073231f51110102001102db9a00007d435101110010000a001700601600007f05000000000000010000000000da951651", 0},
		}},
		// Original WindStrike admission spent MP, then its ordinary owner's
		// passive object produced these encoder results in the same live run
		// (support-origin-dispatch-live1, run2).
		// Source identity is the passive object, not the character who owns it.
		{"ordinary-passive", [4]byte{0x11, 0x02, 0x6f, 0xe7}, [4]byte{0x00, 0xf0, 0x09, 0xbb}, []capture{
			{"010c00fb76c8a811026fe72cfeffffd002000000f009bb", -468},
			{"0214004ea749a400f009bb11026fe700c08244550100001f000a003a00d0020000fc00000000000000000000000000cac912b5", 0},
		}},
	} {
		t.Run(trace.name, func(t *testing.T) {
			for _, reliable := range []bool{false, true} {
				t.Run(map[bool]string{false: "unreliable", true: "reliable"}[reliable], func(t *testing.T) {
					now := time.Unix(100, 0)
					world, self, _, oldVictim := navigationTestWorld(now)
					victim := trace.victim
					before := world.actors[oldVictim]
					before.hp, before.hpKnown = 1101, true
					delete(world.actors, oldVictim)
					world.actors[victim], world.target, world.targetAttackAt = before, victim, now
					ordinarySource := trace.source[0] == 0x11
					if ordinarySource {
						world.actors[trace.source] = partyDungeonActor{hp: 321, hpKnown: true}
					}
					actorCount := len(world.actors)
					r := &RobotVo{partyDungeonWorld: world, partyDungeonEnteredAt: now,
						partySelfPeer: partyIPPeer{uniqueID: 5, slot: 1, slotKnown: true}}
					peer := partyIPPeer{uniqueID: 4, slot: 0, slotKnown: true}
					for i, capture := range trace.captures {
						body := mustPartyHex(t, capture.packet)
						crc := partyPayloadChecksum(body[7:])
						// Original kind-2 packets retain independent high header
						// bytes; only their folded CRC byte is checked. Kind 1
						// carries the complete CRC. Preserve both real headers.
						if body[3] != crc[0] || body[0] == 1 && !bytes.Equal(body[3:7], crc[:]) {
							t.Fatal("original encoded packet checksum differs")
						}
						for j := 7; j < len(body); j++ {
							body[j] ^= 0x7e
						}
						var frame []byte
						if reliable {
							frame = buildPartyReliablePacket(uint32(i), 0, 0, [][]byte{body})
						} else {
							frame = buildPartyUnreliablePacket(uint32(i), 0, 0, body)
						}
						r.observePartyDungeonFrameUnsafe(frame, peer, now.Add(time.Duration(i+1)*time.Second))
						actor := r.partyDungeonWorld.actors[victim]
						if !actor.hpKnown || actor.hp != capture.hp || !actor.dead || actor.position != before.position {
							t.Fatalf("original result was not applied to its victim: %+v", actor)
						}
						if r.partyDungeonWorld.target != [4]byte{} || !r.partyDungeonWorld.targetAttackAt.IsZero() {
							t.Fatal("dead target remained selected")
						}
						source, sourceAdded := r.partyDungeonWorld.actors[trace.source]
						if r.partyDungeonWorld.actors[self].hpKnown || len(r.partyDungeonWorld.actors) != actorCount || sourceAdded != ordinarySource || ordinarySource && (!source.hpKnown || source.hp != 321 || source.dead) {
							t.Fatal("damage was applied to the player or passive source")
						}
					}
				})
			}
		})
	}
}

func TestDungeonOriginalSupportDamageOpaqueFooter(t *testing.T) {
	// The original receiver still applied HP after the four encoder-tail bytes
	// were changed and its complete CRC was recomputed. Keep the product
	// parser's length and identity checks, while treating that tail as opaque.
	packet := mustPartyHex(t, "021400da12e3e600f0f62a1102a7cd00008b4438013d00000000000000301800004d040000000000000000000000002f5f4bc1")
	copy(packet[len(packet)-4:], []byte{0xde, 0xad, 0xbe, 0xef})
	checksum := partyPayloadChecksum(packet[7:])
	copy(packet[3:7], checksum[:])
	clearPacket := append([]byte(nil), packet...)
	for _, reliable := range []bool{false, true} {
		t.Run(map[bool]string{false: "unreliable", true: "reliable"}[reliable], func(t *testing.T) {
			now := time.Unix(100, 0)
			world, self, _, oldVictim := navigationTestWorld(now)
			victim := [4]byte{0x11, 0x02, 0xa7, 0xcd}
			before := world.actors[oldVictim]
			before.hp, before.hpKnown = 1101, true
			delete(world.actors, oldVictim)
			world.actors[victim], world.target, world.targetAttackAt = before, victim, now
			actorCount := len(world.actors)
			r := &RobotVo{partyDungeonWorld: world, partyDungeonEnteredAt: now,
				partySelfPeer: partyIPPeer{uniqueID: 5, slot: 1, slotKnown: true}}
			peer := partyIPPeer{uniqueID: 4, slot: 0, slotKnown: true}
			wirePacket := append([]byte(nil), clearPacket...)
			for i := 7; i < len(wirePacket); i++ {
				wirePacket[i] ^= 0x7e
			}
			var frame []byte
			if reliable {
				frame = buildPartyReliablePacket(0, 0, 0, [][]byte{wirePacket})
			} else {
				frame = buildPartyUnreliablePacket(0, 0, 0, wirePacket)
			}
			r.observePartyDungeonFrameUnsafe(frame, peer, now.Add(time.Second))
			actor := r.partyDungeonWorld.actors[victim]
			if !actor.hpKnown || actor.hp != 0 || !actor.dead || len(r.partyDungeonWorld.actors) != actorCount {
				t.Fatalf("opaque footer changed valid damage result: %+v", actor)
			}
			if r.partyDungeonWorld.actors[self].hpKnown || r.partyDungeonWorld.target != [4]byte{} {
				t.Fatal("opaque-footer damage affected the wrong actor or retained a dead target")
			}
		})
	}
}

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

func TestEnvelopeDamageAndDestroyUpdateOnlyObservedVictim(t *testing.T) {
	wrap := func(command uint16, payload []byte, reliable bool) []byte {
		body := make([]byte, 22+len(payload))
		body[0], body[1], body[7], body[15] = 2, 81, 2, 2
		if reliable {
			body[0], body[7], body[15] = 1, 1, 1
		}
		binary.LittleEndian.PutUint16(body[8:10], command)
		binary.LittleEndian.PutUint16(body[16:18], command)
		copy(body[22:], payload)
		crc := partyPayloadChecksum(payload)
		copy(body[10:14], crc[:])
		copy(body[18:22], crc[:])
		crc = partyPayloadChecksum(body[7:])
		copy(body[3:7], crc[:])
		return body
	}
	for _, reliable := range []bool{false, true} {
		now := time.Now()
		world, self, _, victim := navigationTestWorld(now)
		for _, capture := range dungeonDamageCaptures {
			body := wrap(20, mustPartyHex(t, capture.plain), reliable)
			world.observe(body, now)
			if actor := world.actors[victim]; !actor.hpKnown || actor.hp != capture.hp || actor.dead != (capture.hp == 0) || world.actors[self].hpKnown {
				t.Fatalf("enveloped damage updated the wrong state: %+v", actor)
			}
		}
		world = partyDungeonWorld{}
		valid := wrap(20, mustPartyHex(t, dungeonDamageCaptures[0].plain), reliable)
		for length := 0; length < len(valid); length++ {
			world.observe(valid[:length], now)
		}
		for _, offset := range []int{3, 7, 10, 14, 16, 18, 22} {
			corrupt := append([]byte(nil), valid...)
			corrupt[offset] ^= 1
			world.observe(corrupt, now)
		}
		if len(world.actors) != 0 {
			t.Fatal("invalid envelope changed the world")
		}
		world.observe(wrap(23, victim[:], reliable), now)
		if !world.actors[victim].dead {
			t.Fatal("valid enveloped destroy was ignored")
		}
	}
}
