package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

func TestCapturedPartyResourceAndSkillSequence(t *testing.T) {
	world := partyDungeonWorld{}
	now := time.Now()
	ref := partyDungeonPlayerReference(412)
	for _, packet := range []struct {
		command uint16
		plain   string
		mp      int32
	}{
		{25, "11019c0101016902000000", 617},
		{13, "11019c016b020000", 619},
		{13, "11019c016d020000", 621},
	} {
		payload, err := hex.DecodeString(packet.plain)
		if err != nil {
			t.Fatal(err)
		}
		body := motionCaptureBody(t, packet.plain)
		body[0] = 2
		binary.LittleEndian.PutUint16(body[1:], packet.command)
		world.observe(body, now)
		actor := world.actors[ref]
		if !actor.mpKnown || actor.mp != packet.mp || actor.lastSkill != 1 || actor.skillLevel != 1 || actor.lastSkillAt != now {
			t.Fatalf("command %d: %+v, payload=%x", packet.command, actor, payload)
		}
	}
	if world.actors[ref].hpKnown {
		t.Fatal("MP updates must not initialize HP")
	}
}

func TestCapturedMonsterTargetAndPlayerHitDoNotInventDamage(t *testing.T) {
	world := partyDungeonWorld{}
	now := time.Now()
	monster := [4]byte{0x11, 2, 0xe6, 0x7c}
	player := partyDungeonPlayerReference(413)
	for _, packet := range []struct {
		command uint16
		plain   string
	}{
		{6, "1102e67c9d01"},
		{32, "1102e67c11019d01"},
		{19, "1102e67c11019d0100c08644f7001f0018000a0014000082e328db9965"},
	} {
		body := motionCaptureBody(t, packet.plain)
		body[0] = 2
		binary.LittleEndian.PutUint16(body[1:], packet.command)
		world.observe(body, now)
	}
	if world.actors[monster].controller != 413 || world.actors[monster].attackTarget != player {
		t.Fatalf("monster ownership/target lost: %+v", world.actors[monster])
	}
	if actor := world.actors[player]; actor.lastHitFrom != monster || actor.lastHitAt != now || actor.hpKnown {
		t.Fatalf("hit must retain source without fabricating HP: %+v", actor)
	}
}

func TestPlayerResourcesSurviveRoomChangeWithoutRetainingCoordinates(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	actor := r.partyDungeonWorld.actors[self]
	actor.hp, actor.hpKnown, actor.mp, actor.mpKnown = 749, true, 617, true
	r.partyDungeonWorld.actors[self] = actor
	r.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
	actor = r.partyDungeonWorld.actors[self]
	if actor.hp != 749 || !actor.hpKnown || actor.mp != 617 || !actor.mpKnown || actor.position.known != 0 || actor.teamKnown {
		t.Fatalf("resources or room boundary lost: %+v", actor)
	}
	r.clearPartyDungeonRuntimeUnsafe()
	if len(r.partyDungeonWorld.actors) != 0 {
		t.Fatal("leaving dungeon retained old combat resources")
	}
}
