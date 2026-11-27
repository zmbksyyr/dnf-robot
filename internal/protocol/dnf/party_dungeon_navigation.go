package dnf

import (
	"math"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
)

const partyDungeonNavigationInterval = 200 * time.Millisecond

// A remote member entering a gate can move the whole party. Regroup near a
// stationary leader, keeping clear of the gate they may still be standing in.
// Once they leave the entry area, close up so ordinary exits remain usable.
const partyDungeonFollowDistance = 80
const partyDungeonExitFollowDistance = 0

// partyDungeonNavigationSlack absorbs the phase difference between the client
// frame cadence and the navigation interval. A caller that arrives exactly one
// interval later must be allowed through; without the slack a 200 ms cadence
// lands a hair early every time and the Robot never walks.
const partyDungeonNavigationSlack = 20 * time.Millisecond

// partyDungeonMoveDiagInterval throttles the dungeon movement trace so a long
// run stays readable while still showing whether the Robot actually walks.
const partyDungeonMoveDiagInterval = 5 * time.Second

type partyDungeonNavigation struct {
	nextAt          time.Time
	active          bool
	leaderEntry     partyDungeonPosition
	leaderLeftEntry bool
	// reason records why the last plan did or did not move; it only feeds the
	// throttled diagnostic trace.
	reason string
	// mode says what the plan was aiming at: a live monster, or the leader once
	// the room is clear. It only feeds the trace.
	mode string
}

// plan approaches attack distance, then follows the leader when no live target
// remains. Once in range, an approaching monster must not push the Robot into
// continually retreating to preserve an exact offset.
func (world *partyDungeonWorld) planNavigation(self, leader [4]byte, now time.Time) (partyDungeonPosition, bool, bool) {
	position := world.actors[self].position
	leaderActor := world.actors[leader]
	if leaderActor.position.known&3 == 3 && (!world.entryFromKnown || leaderActor.room != world.entryFrom) &&
		(position.known&3 != 3 || world.actors[self].room == leaderActor.room) {
		navigation := &world.navigation
		if navigation.leaderEntry.known&3 != 3 {
			navigation.leaderEntry = leaderActor.position
		} else if math.Hypot(float64(leaderActor.position.x-navigation.leaderEntry.x), float64(leaderActor.position.y-navigation.leaderEntry.y)) > partyDungeonFollowDistance {
			navigation.leaderLeftEntry = true
		}
	}
	if position.known&3 != 3 {
		world.navigation.reason = "self-unknown"
		if !world.entryFromKnown {
			return position, false, false
		}
		// The server resets our room position before the new leader motion
		// arrives. Suppress delayed old-room replay while awaiting a complete
		// position in the adjacent room, then stage inside its entrance. Exact
		// replay would put us in the gate occupied by the stationary leader.
		actor := world.actors[leader]
		dx := int(actor.room[0]) - int(world.entryFrom[0])
		dy := int(actor.room[1]) - int(world.entryFrom[1])
		if actor.position.known&3 != 3 || dx*dx+dy*dy != 1 {
			world.navigation.reason = "await-room-position"
			return position, false, true
		}
		position = actor.position
		position.x += float32(dx * partyDungeonFollowDistance)
		position.y += float32(dy * partyDungeonFollowDistance)
		position.z, position.known = 0, 7
		world.navigation.reason = "enter-room"
		return position, true, true
	}
	if actor := world.actors[leader]; !actor.seenAt.IsZero() && world.actors[self].room != actor.room {
		// Peer UDP can precede the server's room notification. Wait for the
		// reset instead of moving old-room coordinates into the new room.
		world.navigation.reason = "await-room-loading"
		return position, false, true
	}
	world.reconsiderTarget(self, position, now)
	_, target, enemy := world.nearestTarget(self, position)
	navigation := &world.navigation
	navigation.mode = "follow"
	followDistance := float64(partyDungeonFollowDistance)
	if enemy {
		navigation.mode = "chase"
	}
	if !enemy {
		if navigation.leaderLeftEntry {
			followDistance = partyDungeonExitFollowDistance
		}
		target = world.actors[leader].position
		if target.known&3 != 3 {
			navigation.reason = "leader-unknown"
			return position, false, true
		}
		if !navigation.leaderLeftEntry && navigation.leaderEntry.known&3 == 3 && world.entryFromKnown {
			room := world.actors[leader].room
			dx, dy := int(room[0])-int(world.entryFrom[0]), int(room[1])-int(world.entryFrom[1])
			if dx*dx+dy*dy == 1 {
				// Regroup on the same interior point used for initialization.
				// A radial stop around the leader can still enter a wide gate
				// when returning from the side of the room.
				target = navigation.leaderEntry
				target.x += float32(dx * partyDungeonFollowDistance)
				target.y += float32(dy * partyDungeonFollowDistance)
				followDistance = 0
			}
		}
	}
	navigation.active = true
	if now.Add(partyDungeonNavigationSlack).Before(navigation.nextAt) {
		navigation.reason = "tick-wait"
		return position, false, true
	}
	navigation.nextAt = now.Add(partyDungeonNavigationInterval)
	if enemy {
		switch {
		case target.x-position.x > 70:
			target.x -= 70
		case position.x-target.x > 70:
			target.x += 70
		default:
			target.x = position.x
		}
	}
	dx, dy := float64(target.x-position.x), float64(target.y-position.y)
	if math.Abs(dx) <= 8 && math.Abs(dy) <= 6 {
		navigation.reason = "in-range"
		return position, false, true
	}
	distance := math.Hypot(dx, dy)
	if !enemy {
		if distance <= followDistance+0.01 {
			navigation.reason = "in-range"
			return position, false, true
		}
		// Cap the final step at the follow radius as well; a full walking step
		// from just outside it would still approach the entrance too closely.
		step := math.Min(40, distance-followDistance)
		dx *= step / distance
		dy *= step / distance
	} else if distance > 40 {
		// Match the bounded walking step used by the real-client movement probe.
		// Delayed scheduling never accumulates a teleport-sized catch-up step.
		dx *= 40 / distance
		dy *= 40 / distance
	}
	position.x += float32(dx)
	position.y += float32(dy)
	position.z, position.known = 0, 7
	world.navigation.reason = "move"
	return position, true, true
}

func (r *RobotVo) navigatePartyDungeonUnsafe(conn *net.UDPConn, now time.Time) bool {
	if r.partyDungeonLoading {
		return false
	}
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout || r.partyDungeonLastAt.IsZero() || now.Sub(r.partyDungeonLastAt) > partyDungeonActivityTimeout {
		return false
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) {
		return false
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	before := r.partyDungeonWorld.actors[self].position
	position, move, active := r.partyDungeonWorld.planNavigation(self, partyDungeonPlayerReference(peer.uniqueID), now)
	if !now.Before(r.partyDungeonMoveDiagAt) {
		r.partyDungeonMoveDiagAt = now.Add(partyDungeonMoveDiagInterval)
		leader := r.partyDungeonWorld.actors[partyDungeonPlayerReference(peer.uniqueID)].position
		navigation := &r.partyDungeonWorld.navigation
		// The leader's position can differ from the guarded interior point.
		// Include the actual next step and entry state so an intentional hold
		// is distinguishable from a failure to follow a departing leader.
		foundationlog.Robotf("[PARTY_DUNGEON_NAV] uid=%d self=(%d) leader=(%d) from=(%.0f,%.0f) to=(%.0f,%.0f) next=(%.0f,%.0f) entry=(%d,%.0f,%.0f) left_entry=%t mode=%s active=%t move=%t reason=%s\n",
			r.UID, before.known, leader.known, before.x, before.y, leader.x, leader.y, position.x, position.y,
			navigation.leaderEntry.known, navigation.leaderEntry.x, navigation.leaderEntry.y, navigation.leaderLeftEntry,
			navigation.mode, active, move, navigation.reason)
	}
	if !active {
		return false
	}
	// Leader state replay must not overwrite the independent approach. Its
	// latest position is already retained in the room's observed world.
	r.partyDungeonFollow = nil
	if !move {
		return true
	}
	room := r.partyDungeonWorld.actors[partyDungeonPlayerReference(peer.uniqueID)].room
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0x415555, flags: [3]byte{room[0], room[1], 0x10}}
	if position.x > before.x {
		entry.flags[2] |= 0x20
	}
	for i, value := range []float32{position.x, position.y} {
		entry.modes &^= 3 << uint((9+i)*2)
		entry.values[9+i] = math.Float32bits(value)
	}
	body, ok := buildPartyDungeonMotion([]partyDungeonMotion{entry})
	if !ok {
		return true
	}
	route := r.partyRouteForPeerUnsafe(peer.slot)
	if !r.partyRouteSendEligibleUnsafe(conn, peer, route, now) {
		return true
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
	if !ok {
		return true
	}
	packet, err := buildPartyUnreliablePacketChecked(sequence, r.partySelfPeer.slot, r.partyDungeonFlags, body)
	if err != nil {
		return true
	}
	if _, err := r.sendPartyTransportUnsafe(conn, peer, route, packet); err == nil {
		r.partyDungeonWorld.observe(body, now)
		if !now.Before(r.partyDungeonSentDiagAt) {
			r.partyDungeonSentDiagAt = now.Add(partyDungeonMoveDiagInterval)
			foundationlog.Robotf("[PARTY_DUNGEON_MOVE] uid=%d peer=%d from=(%.0f,%.0f) to=(%.0f,%.0f)\n",
				r.UID, peer.accID, before.x, before.y, position.x, position.y)
		}
	}
	return true
}
