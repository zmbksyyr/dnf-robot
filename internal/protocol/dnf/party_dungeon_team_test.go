package dnf

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// Constructed from the native three-word reader, not a real SET_TEAM capture.
func dungeonTeamTestBody(payload []byte, envelope, reliable bool) []byte {
	kind := byte(2)
	if reliable {
		kind = 1
	}
	if !envelope {
		body := make([]byte, 7+len(payload))
		body[0], body[1] = kind, 54
		crc := partyPayloadChecksum(payload)
		copy(body[3:7], crc[:])
		for i, b := range payload {
			body[7+i] = b ^ 0x7e
		}
		return body
	}
	body := make([]byte, 22+len(payload))
	body[0], body[1], body[7], body[15] = kind, 81, kind, kind
	binary.LittleEndian.PutUint16(body[8:10], 54)
	binary.LittleEndian.PutUint16(body[16:18], 54)
	copy(body[22:], payload)
	crc := partyPayloadChecksum(payload)
	copy(body[10:14], crc[:])
	copy(body[18:22], crc[:])
	crc = partyPayloadChecksum(body[7:])
	copy(body[3:7], crc[:])
	return body
}

func observeDungeonTeam(world *partyDungeonWorld, ref [4]byte, team uint16, now time.Time) {
	payload := make([]byte, 6)
	copy(payload, ref[:])
	binary.LittleEndian.PutUint16(payload[4:], team)
	world.observe(dungeonTeamTestBody(payload, false, true), now)
}

func TestDungeonSetTeamNativeWordLayoutAndInvalidPackets(t *testing.T) {
	for _, envelope := range []bool{false, true} {
		for _, reliable := range []bool{false, true} {
			t.Run(fmt.Sprintf("envelope=%t/reliable=%t", envelope, reliable), func(t *testing.T) {
				now := time.Unix(100, 0)
				ref := [4]byte{0x11, 2, 0x78, 0x56}
				plain := []byte{0x11, 2, 0x78, 0x56, 0x34, 0x12}
				valid := dungeonTeamTestBody(plain, envelope, reliable)
				world := partyDungeonWorld{}
				for n := 0; n < len(valid); n++ {
					world.observe(valid[:n], now)
				}
				bad := append([]byte(nil), valid...)
				bad[len(bad)-1] ^= 1
				world.observe(bad, now)
				bad = append([]byte(nil), valid...)
				bad[3] ^= 1
				world.observe(bad, now)
				// Correct checksums must not make another payload length valid.
				world.observe(dungeonTeamTestBody(plain[:5], envelope, reliable), now)
				world.observe(dungeonTeamTestBody(append(append([]byte(nil), plain...), 0), envelope, reliable), now)
				wrongClass := append([]byte(nil), plain...)
				wrongClass[1] = 0xf0
				world.observe(dungeonTeamTestBody(wrongClass, envelope, reliable), now)
				if len(world.actors) != 0 {
					t.Fatal("invalid team packet created an actor")
				}
				world.observe(valid, now)
				a := world.actors[ref]
				if len(world.actors) != 1 || !a.teamKnown || a.team != 0x1234 || a.seenAt != now || a.position.known != 0 || a.hpKnown {
					t.Fatalf("SET_TEAM did not retain exactly the native words: %+v", a)
				}
			})
		}
	}
}

func TestDungeonTeamUpdatesRevalidateTargetAndPreserveUnknownTeams(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, _, enemy := navigationTestWorld(now)
	position := world.actors[self].position
	assertTarget := func(want bool) {
		t.Helper()
		ref, _, ok := world.nearestTarget(self, position)
		if ok != want || want && ref != enemy {
			t.Fatalf("target=%x ok=%t want=%t", ref, ok, want)
		}
	}
	assertTarget(true)
	observeDungeonTeam(&world, enemy, 0, now)
	assertTarget(true) // No received owner team; do not invent team zero.
	observeDungeonTeam(&world, self, 0, now)
	assertTarget(false) // Revalidate the cached target, including team zero.
	if !world.targetAttackAt.IsZero() || world.target != [4]byte{} {
		t.Fatal("friendly target retained combat progress")
	}
	observeDungeonTeam(&world, enemy, 100, now)
	assertTarget(true)
	world.noteTargetAttack(enemy, now)
	observeDungeonTeam(&world, self, 100, now)
	assertTarget(false) // An owner update must also invalidate the cached target.
	observeDungeonTeam(&world, self, 0, now)
	assertTarget(true)
	before := world.actors[enemy]
	world.observeActorEvent(23, enemy[:], now)
	observeDungeonTeam(&world, enemy, 101, now)
	assertTarget(false)
	if a := world.actors[enemy]; !a.dead || a.position != before.position || a.hp != before.hp || a.hpKnown != before.hpKnown {
		t.Fatal("team update altered lifetime, motion or HP")
	}
}

func TestDungeonTeamExclusionAppliesToProgressRotationAndObjectives(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, _, enemy := navigationTestWorld(now)
	friendly := [4]byte{0x11, 2, 2, 3}
	world.actors[friendly] = world.actors[enemy]
	observeDungeonTeam(&world, self, 0, now)
	observeDungeonTeam(&world, friendly, 0, now)
	observeDungeonTeam(&world, enemy, 100, now)
	world.target = enemy
	world.noteTargetAttack(enemy, now)
	world.noteTargetAttack(enemy, now.Add(16*time.Second))
	world.reconsiderTarget(self, world.actors[self].position, now.Add(16*time.Second))
	if world.target != enemy {
		t.Fatal("no-damage rotation selected a friendly actor")
	}
	objective := [4]byte{0x21, 0, 2, 0}
	world.actors[objective] = partyDungeonActor{position: world.actors[enemy].position, room: world.actors[self].room, objective: true}
	observeDungeonTeam(&world, objective, 0, now)
	world.reconsiderTarget(self, world.actors[self].position, now.Add(16*time.Second))
	if world.target != objective {
		t.Fatal("team metadata changed the authored objective rule")
	}
}

func TestDungeonTeamChangeStopsComboAndActiveCast(t *testing.T) {
	now := time.Now()
	r, self, enemy := basicAttackTestRobot(t, now)
	r.partySkillJob = 8
	r.partyDungeonWorld.target = enemy
	r.partyBasicAttackStep, r.partyBasicAttackTarget = 1, enemy
	observeDungeonTeam(&r.partyDungeonWorld, self, 0, now)
	observeDungeonTeam(&r.partyDungeonWorld, enemy, 0, now)
	if r.tryPartyBasicAttackUnsafe(nil, now) || r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
		t.Fatal("combo continued against an actor that changed to our team")
	}
	if r.sendPartySkillStateUnsafe(nil, now, 22, []byte{3, 0, 0}, "CAST") || r.partyTQOSReliableSeq[0][1] != 0 || len(r.partySkillReadyAt) != 0 {
		t.Fatal("friendly-only room sent a cast or consumed cooldown")
	}
}

func TestDungeonTeamMetadataDoesNotSurviveRoomOrSessionReset(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, _, enemy := navigationTestWorld(now)
	observeDungeonTeam(&world, self, 0, now)
	observeDungeonTeam(&world, enemy, 100, now)
	for _, room := range []bool{false, true} {
		r := &RobotVo{partyDungeonWorld: world, partyDungeonEnteredAt: now}
		if room {
			r.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
		} else {
			r.clearPartyDungeonRuntimeUnsafe()
		}
		if len(r.partyDungeonWorld.actors) != 0 {
			t.Fatal("team metadata leaked into another room or session")
		}
	}
}
