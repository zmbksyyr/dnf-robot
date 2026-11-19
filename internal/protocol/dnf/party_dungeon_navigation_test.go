package dnf

import (
	"encoding/binary"
	"math"
	"net"
	"testing"
	"time"
)

func navigationTestWorld(now time.Time) (partyDungeonWorld, [4]byte, [4]byte, [4]byte) {
	self, leader, enemy := partyDungeonPlayerReference(5), partyDungeonPlayerReference(4), [4]byte{0x11, 2, 1, 3}
	return partyDungeonWorld{actors: map[[4]byte]partyDungeonActor{
		self:   {position: partyDungeonPosition{x: 100, y: 300, known: 7}, seenAt: now},
		leader: {position: partyDungeonPosition{x: 500, y: 300, known: 7}, seenAt: now},
		enemy:  {position: partyDungeonPosition{x: 300, y: 300, known: 7}, seenAt: now},
	}}, self, leader, enemy
}

func TestNavigationKeepsTargetAndApproachSideUnderCrowding(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	var last partyDungeonPosition
	for i := 0; i < 12; i++ {
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
		world.actors[[4]byte{0x11, 2, 2, 3}] = partyDungeonActor{position: partyDungeonPosition{x: position.x + 10, y: 300, known: 7}, seenAt: at}
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
	if _, _, active := world.planNavigation(self, leader, now); active {
		t.Fatal("navigation replaced existing follow before observing combat")
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
