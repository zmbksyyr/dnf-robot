package dnf

import (
	"net"
	"testing"
	"time"
)

// The dungeon loop is one state machine: enter a room, pick a live target and
// attack it, treat an empty room as cleared and regroup on the leader, reset on
// the next room, and drop the whole runtime when the party returns to town. The
// individual transitions have focused tests; this one walks the sequence so a
// regression in any link shows up as a broken loop.
func TestPartyDungeonLoopEnterAttackClearNextRoomTown(t *testing.T) {
	now := time.Unix(500, 0)
	r, self, enemy := basicAttackTestRobot(now)
	r.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 9, state: 23, level: 1, stateData: []byte{0, 1}, risk: 0},
	}
	r.partySkillReadyAt = map[byte]time.Time{}
	r.partySkillNextAt = now

	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	remote := receiver.LocalAddr().(*net.UDPAddr)
	r.partyPeers[0].outerIP, r.partyPeers[0].port = remote.IP, uint16(remote.Port)

	readFrame := func() []byte {
		buf := make([]byte, 4096)
		receiver.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := receiver.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("expected a dungeon frame: %v", err)
		}
		return buf[:n]
	}
	drain := func() {
		buf := make([]byte, 4096)
		for {
			receiver.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
			if _, _, err := receiver.ReadFromUDP(buf); err != nil {
				return
			}
		}
	}

	// 1. First room: the robot faces and casts at the live monster.
	r.flushPartyDungeonSkillUnsafe(sender, now)
	frame := readFrame()
	bodies, ok := partyDungeonFrameBodies(frame)
	if !ok || len(bodies) != 2 {
		t.Fatalf("cast frame = %x", frame)
	}
	if motion, ok := decodePartyDungeonMotion(bodies[0]); !ok || len(motion) != 1 || motion[0].entity != self {
		t.Fatalf("cast was not preceded by facing: %+v", motion)
	}
	// This candidate has no PVF cooldown metadata, so the loop falls back to the
	// action delay instead of inventing one; the cast still enters recovery.
	if r.partySkillRecoverAt.IsZero() {
		t.Fatal("successful cast did not enter recovery")
	}
	if _, recorded := r.partySkillReadyAt[9]; recorded {
		t.Fatal("cast invented a cooldown without PVF metadata")
	}
	// Recovery has to be acknowledged before the next action.
	r.partySkillRecoverAt = now.Add(-time.Millisecond)
	r.sendPartySkillStateUnsafe(sender, now.Add(time.Second), 0, nil, "RECOVER")
	r.partySkillRecoverAt = time.Time{}
	drain()

	// 2. Clearing the room: while a target is alive the loop keeps navigating
	// (which arms the approach state); once it dies the same path regroups on
	// the leader instead of standing still or re-attacking a corpse.
	r.navigatePartyDungeonUnsafe(sender, now.Add(time.Second))
	actor := r.partyDungeonWorld.actors[enemy]
	actor.dead = true
	r.partyDungeonWorld.actors[enemy] = actor
	r.partyDungeonWorld.target = [4]byte{}
	r.partySkillNextAt = now.Add(time.Hour)

	before := r.partyDungeonWorld.actors[self].position
	marker := r.partyDungeonFollow
	if !r.navigatePartyDungeonUnsafe(sender, now.Add(2*time.Second)) {
		t.Fatal("cleared room did not start regrouping")
	}
	if len(r.partyDungeonFollow) != 0 && marker != nil {
		t.Fatal("navigation kept the leader replay queued")
	}
	moveFrame := readFrame()
	moveBodies, ok := partyDungeonFrameBodies(moveFrame)
	if !ok || len(moveBodies) != 1 {
		t.Fatalf("regroup frame = %x", moveFrame)
	}
	motion, ok := decodePartyDungeonMotion(moveBodies[0])
	if !ok || len(motion) != 1 || motion[0].entity != self {
		t.Fatalf("regroup motion = %+v", motion)
	}
	after := before.apply(motion[0])
	if after.x == before.x && after.y == before.y {
		t.Fatalf("regroup did not move: %+v -> %+v", before, after)
	}

	// 3. The next room resets the room's actors and every per-room action.
	r.markPartyDungeonEnteredUnsafe(now.Add(3 * time.Second))
	if len(r.partyDungeonWorld.actors) != 0 || len(r.partyDungeonFollow) != 0 {
		t.Fatalf("next room kept stale state: actors=%d follow=%d", len(r.partyDungeonWorld.actors), len(r.partyDungeonFollow))
	}
	if r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != ([4]byte{}) {
		t.Fatalf("next room kept the attack combo: step=%d target=%x", r.partyBasicAttackStep, r.partyBasicAttackTarget)
	}
	if !r.partySkillRecoverAt.IsZero() {
		t.Fatalf("next room kept the recovery timer: %s", r.partySkillRecoverAt)
	}
	if r.partyDungeonEnteredAt.IsZero() {
		t.Fatal("next room did not keep the dungeon session alive")
	}

	// 4. Back to town: the dungeon runtime must not survive the transition.
	r.clearPartyDungeonRuntimeUnsafe()
	if !r.partyDungeonEnteredAt.IsZero() || !r.partyDungeonLastAt.IsZero() || r.partyDungeonFlags != 0 {
		t.Fatalf("town kept the dungeon runtime: entered=%s last=%s flags=%d", r.partyDungeonEnteredAt, r.partyDungeonLastAt, r.partyDungeonFlags)
	}
	if !r.partySkillNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
		t.Fatalf("town kept casting timers: next=%s recover=%s", r.partySkillNextAt, r.partySkillRecoverAt)
	}
	if r.partyDungeonLegacySkills {
		t.Fatal("town kept the legacy skill transport choice")
	}
}
