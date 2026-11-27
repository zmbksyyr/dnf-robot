package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"net"
	"testing"
	"time"
)

func TestNavigationAndAttackPreserveObservedRoomCoordinates(t *testing.T) {
	// Real Linux leader motion on 2026-11-22, entering grid (1,1), then
	// (1,2). The old hard-coded (2,0) put the Robot in another room.
	for _, tc := range []struct {
		wire string
		room [2]byte
	}{
		{"010400993b9b8a7f6f7f6c7e6f7f6c7e7f7f4e2b2b3f7ebe433a7e7e683d", [2]byte{1, 1}},
		{"010400cb1e50b57f6f7f6c7e6f7f6c7e7f7c6e2b2b3f7efe313a7e7e683d", [2]byte{1, 2}},
	} {
		now := time.Now()
		body, err := hex.DecodeString(tc.wire)
		if err != nil {
			t.Fatal(err)
		}
		world := partyDungeonWorld{}
		world.observe(body, now)
		leader := world.actors[partyDungeonPlayerReference(18)]
		if leader.room != tc.room || leader.position.known != 7 {
			t.Fatalf("captured leader room was not observed: %+v", leader)
		}
		facing, ok := buildPartyAttackFacing(19, leader.position, partyDungeonPosition{x: leader.position.x + 70, y: leader.position.y, known: 7}, leader.room)
		if !ok {
			t.Fatal("facing unavailable")
		}
		world.observe(facing, now)
		self := world.actors[partyDungeonPlayerReference(19)]
		if self.room != tc.room || self.position != leader.position {
			t.Fatalf("facing changed the observed room/position: %+v", self)
		}
	}
}

func navigationTestWorld(now time.Time) (partyDungeonWorld, [4]byte, [4]byte, [4]byte) {
	self, leader, enemy := partyDungeonPlayerReference(5), partyDungeonPlayerReference(4), [4]byte{0x11, 2, 1, 3}
	return partyDungeonWorld{actors: map[[4]byte]partyDungeonActor{
		self:   {position: partyDungeonPosition{x: 100, y: 300, known: 7}, room: [2]byte{1, 2}, seenAt: now},
		leader: {position: partyDungeonPosition{x: 500, y: 300, known: 7}, room: [2]byte{1, 2}, seenAt: now},
		enemy:  {position: partyDungeonPosition{x: 300, y: 300, known: 7}, room: [2]byte{1, 2}, seenAt: now},
	}}, self, leader, enemy
}

func TestNavigationKeepsTargetAndApproachSideUnderCrowding(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	var last partyDungeonPosition
	// Stationary actors need no periodic motion update. Keep this approach
	// active longer than the previously imposed three-second expiry.
	for i := 0; i < 70; i++ {
		at := now.Add(time.Duration(i) * partyDungeonNavigationInterval)
		position, move, active := world.planNavigation(self, leader, at)
		if !active || position.x > 230 || position.x < world.actors[self].position.x || position.y != 300 {
			t.Fatalf("approach crossed target or reversed: tick=%d position=%+v", i, position)
		}
		if move {
			actor := world.actors[self]
			actor.position = position
			world.actors[self] = actor
		}
		// A nearer monster crossing the Robot must not continually replace the
		// chosen enemy or reverse the chosen side of its attack position.
		world.actors[[4]byte{0x11, 2, 2, 3}] = partyDungeonActor{position: partyDungeonPosition{x: position.x + 10, y: 300, known: 7}, room: [2]byte{1, 2}, seenAt: at}
		if world.target != enemy {
			t.Fatalf("target changed during approach: %x", world.target)
		}
		last = position
	}
	if math.Abs(float64(last.x-230)) > 8 {
		t.Fatalf("did not settle at attack distance: %+v", last)
	}
}

func TestNavigationHoldsAttackRangeInsteadOfRetreatingFromApproachingEnemy(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	actor := world.actors[self]
	actor.position.x = 230
	world.actors[self] = actor
	for i, x := range []float32{300, 280, 250, 230, 200, 165} {
		at := now.Add(time.Duration(i) * partyDungeonNavigationInterval)
		actor = world.actors[enemy]
		actor.position.x, actor.seenAt = x, at
		world.actors[enemy] = actor
		position, move, active := world.planNavigation(self, leader, at)
		if move || !active || position.x != 230 {
			t.Fatalf("retreated or crossed a nearby enemy at x=%f: %+v", x, position)
		}
	}
}

func TestClearedRoomFollowStopsBeforeStationaryLeaderAtEntrance(t *testing.T) {
	// Actual Windows 2026-11-22 room (2,1): the leader remained at its upper
	// entrance (1265,151). After clearing, our next step from this captured
	// position entered the gate and took the whole party back to (2,0).
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	delete(world.actors, enemy)
	captured, err := hex.DecodeString("010400f33ac6f47f6f7f7d7e6f7f7d7e7c7f4e2b2b3fb465ea3ab9d35a3d")
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := decodePartyDungeonMotion(captured)
	if !ok || len(entries) != 1 {
		t.Fatal("invalid real follower motion")
	}
	actor := world.actors[self]
	actor.position = (partyDungeonPosition{}).apply(entries[0])
	initialPosition := actor.position
	world.actors[self] = actor
	actor = world.actors[leader]
	actor.position = partyDungeonPosition{x: 1265, y: 151, known: 7}
	world.actors[leader] = actor
	for _, combatObserved := range []bool{false, true} {
		world.navigation = partyDungeonNavigation{active: combatObserved}
		actor := world.actors[self]
		actor.position = initialPosition
		world.actors[self] = actor
		for i := 0; i < 20; i++ {
			position, move, active := world.planNavigation(self, leader, now.Add(time.Duration(i)*partyDungeonNavigationInterval))
			distance := math.Hypot(float64(position.x-1265), float64(position.y-151))
			if !active || distance < 79.99 {
				t.Fatalf("follow crossed into the entrance or fell back to exact replay: combat=%t step=%d distance=%f", combatObserved, i, distance)
			}
			if i == 19 && move {
				t.Fatal("stationary follow kept emitting motion after reaching its spacing")
			}
			if move {
				actor := world.actors[self]
				actor.position = position
				world.actors[self] = actor
			}
		}
	}
	// The leader walking onward still pulls the follower along; spacing is
	// not a permanent hold at this room's entrance.
	actor = world.actors[leader]
	actor.position.x += 200
	world.actors[leader] = actor
	before := world.actors[self].position
	position, move, active := world.planNavigation(self, leader, now.Add(10*time.Second))
	if !active || !move || position.x <= before.x {
		t.Fatal("follower did not resume after the leader moved onward")
	}
}

func TestClearedRoomFollowClosesUpAfterLeaderLeavesEntry(t *testing.T) {
	// Windows map212: the human had walked from the upper entrance
	// (830,151) to the right gate. Keeping 80 pixels forever left our
	// member outside the gate while the human was blocked at its edge.
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	delete(world.actors, enemy)
	actor := world.actors[leader]
	actor.position = partyDungeonPosition{x: 830, y: 151, known: 7}
	world.actors[leader] = actor
	world.planNavigation(self, leader, now)
	actor.position = partyDungeonPosition{x: 1499, y: 268.82114, known: 7}
	world.actors[leader] = actor
	actor = world.actors[self]
	actor.position = partyDungeonPosition{x: 1419.5101, y: 270.33588, known: 7}
	world.actors[self] = actor
	for i := 1; i <= 5; i++ {
		position, move, active := world.planNavigation(self, leader, now.Add(time.Duration(i)*partyDungeonNavigationInterval))
		if !active {
			t.Fatal("departed leader follow fell back to replay")
		}
		if move {
			actor.position = position
			world.actors[self] = actor
		}
	}
	if distance := math.Hypot(float64(actor.position.x-1499), float64(actor.position.y-268.82114)); distance > 10 {
		t.Fatalf("member stayed outside the active exit: distance=%f", distance)
	}
}

func TestClearedRoomFollowReachesUpperGateAfterLeaderDepartsEntry(t *testing.T) {
	// Windows map213 on 2026-11-22: after walking into the room and back,
	// the human stopped at y=148.5058, but our 24-pixel spacing stopped at
	// y=172.5058 and the party never entered the upper gate.
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	delete(world.actors, enemy)
	world.navigation = partyDungeonNavigation{
		leaderEntry: partyDungeonPosition{x: 1265, y: 151, known: 7}, leaderLeftEntry: true,
	}
	actor := world.actors[leader]
	actor.position = partyDungeonPosition{x: 1265, y: 148.5058136, known: 7}
	world.actors[leader] = actor
	actor = world.actors[self]
	actor.position = partyDungeonPosition{x: 1265, y: 172.5058136, known: 7}
	world.actors[self] = actor
	for i := 0; i < 3; i++ {
		position, move, active := world.planNavigation(self, leader, now.Add(time.Duration(i)*partyDungeonNavigationInterval))
		if !active {
			t.Fatal("active return fell back to replay")
		}
		if move {
			actor.position = position
			world.actors[self] = actor
		}
	}
	if math.Abs(float64(actor.position.y-world.actors[leader].position.y)) > 6 {
		t.Fatalf("member stopped outside the observed upper entrance: %+v", actor.position)
	}
}

func TestStationaryEntryRegroupApproachesInsidePointFromTheSide(t *testing.T) {
	// Windows n202 on 2026-11-22: after a real clear, the Robot approached
	// the stationary bottom-entrance leader from the left. A radial stop at
	// (1164.47,331.37), 80 pixels away, still triggered that return gate.
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	delete(world.actors, enemy)
	world.entryFrom, world.entryFromKnown = [2]byte{2, 1}, true
	actor := world.actors[leader]
	actor.room, actor.position = [2]byte{2, 0}, partyDungeonPosition{x: 1244, y: 340, known: 7}
	world.actors[leader] = actor
	actor = world.actors[self]
	actor.room, actor.position = [2]byte{2, 0}, partyDungeonPosition{x: 256.86816, y: 232.92935, known: 7}
	world.actors[self] = actor
	for i := 0; i < 65; i++ {
		position, move, active := world.planNavigation(self, leader, now.Add(time.Duration(i)*partyDungeonNavigationInterval))
		if !active || position.x > 1140 && position.y > 300 {
			t.Fatalf("sideways regroup crossed the entrance trigger: %+v", position)
		}
		if move {
			actor.position = position
			world.actors[self] = actor
		}
	}
	if math.Abs(float64(actor.position.x-1244)) > 8 || math.Abs(float64(actor.position.y-260)) > 6 {
		t.Fatalf("did not settle inside the room: %+v", actor.position)
	}
}

func TestNavigationFollowsAfterDeathAndResetsAtRoomBoundary(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	world.planNavigation(self, leader, now)
	actor := world.actors[enemy]
	actor.dead = true
	world.actors[enemy] = actor
	position, move, active := world.planNavigation(self, leader, now.Add(time.Second))
	if !active || !move || position.x <= 100 || world.target != [4]byte{} {
		t.Fatalf("did not approach leader after last enemy died: %+v target=%x", position, world.target)
	}
	vo := &RobotVo{partyDungeonWorld: world}
	vo.markPartyDungeonEnteredUnsafe(now.Add(2 * time.Second))
	if vo.partyDungeonWorld.navigation.active || !vo.partyDungeonWorld.navigation.nextAt.IsZero() {
		t.Fatal("navigation leaked across rooms")
	}
	world, self, leader, enemy = navigationTestWorld(now)
	delete(world.actors, enemy)
	if _, move, active := world.planNavigation(self, leader, now); !active || !move {
		t.Fatal("already cleared room did not use independent follow navigation")
	}
}

func TestNavigationBoundsLateTicksAndRequiresKnownPosition(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	actor := world.actors[enemy]
	actor.position.x, actor.position.y = 1000, 700
	world.actors[enemy] = actor
	for _, at := range []time.Time{now, now.Add(200 * time.Millisecond), now.Add(2 * time.Second)} {
		before := world.actors[self].position
		position, move, active := world.planNavigation(self, leader, at)
		if !move || !active || math.Hypot(float64(position.x-before.x), float64(position.y-before.y)) > 40.001 {
			t.Fatalf("late tick teleported: before=%+v after=%+v", before, position)
		}
		actor := world.actors[self]
		actor.position = position
		world.actors[self] = actor
		if _, move, _ := world.planNavigation(self, leader, at.Add(time.Millisecond)); move {
			t.Fatal("duplicate tick moved again")
		}
	}
	actor = world.actors[self]
	actor.position.known = 1
	world.actors[self] = actor
	if _, move, _ := world.planNavigation(self, leader, now.Add(3*time.Second)); move {
		t.Fatal("invented an unknown position")
	}
}

func TestNavigationSendsOwnMotionAndDoesNotCommitFailedSend(t *testing.T) {
	now := time.Now()
	world, self, leader, _ := navigationTestWorld(now)
	vo := &RobotVo{partyDungeonWorld: world, partyDungeonEnteredAt: now, partyDungeonLastAt: now,
		partySelfPeer:      partyIPPeer{uniqueID: binary.LittleEndian.Uint16(self[2:]), slot: 1, slotKnown: true},
		partyDungeonFollow: []partyDungeonFollowPending{{body: []byte{1}}},
	}
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
	vo.partyPeers[0] = partyIPPeer{uniqueID: binary.LittleEndian.Uint16(leader[2:]), slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	if !vo.navigatePartyDungeonUnsafe(sender, now) {
		t.Fatal("navigation not active")
	}
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := decodePartyDungeonMotion(buf[9:n])
	if !ok || len(entries) != 1 || entries[0].entity != self || entries[0].controller != self || buf[0] != 2 || buf[7] != 1 {
		t.Fatalf("navigation sent another actor/controller or wrong channel: %x", buf[:n])
	}
	if entries[0].flags[0] != 1 || entries[0].flags[1] != 2 {
		t.Fatalf("navigation moved actor to another room: %+v", entries[0])
	}
	if len(vo.partyDungeonFollow) != 0 || vo.partyDungeonWorld.actors[self].position.x <= 100 {
		t.Fatal("successful navigation did not supersede leader replay")
	}
	before := vo.partyDungeonWorld.actors[self].position
	sender.Close()
	vo.navigatePartyDungeonUnsafe(sender, now.Add(partyDungeonNavigationInterval))
	if vo.partyDungeonWorld.actors[self].position != before {
		t.Fatal("failed send committed an unsent position")
	}
}
