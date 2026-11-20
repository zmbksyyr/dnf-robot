package dnf

import (
	"math"
	"net"
	"time"
)

const partyDungeonNavigationInterval = 200 * time.Millisecond

type partyDungeonNavigation struct {
	nextAt time.Time
	active bool
}

// plan approaches attack distance, then follows the leader when no live target
// remains. Once in range, an approaching monster must not push the Robot into
// continually retreating to preserve an exact offset.
func (world *partyDungeonWorld) planNavigation(self, leader [4]byte, now time.Time) (partyDungeonPosition, bool, bool) {
	position := world.actors[self].position
	if position.known&3 != 3 {
		return position, false, false
	}
	_, target, enemy := world.nearestMonster(position)
	navigation := &world.navigation
	if !enemy {
		if !navigation.active {
			return position, false, false
		}
		target = world.actors[leader].position
		if target.known&3 != 3 {
			return position, false, true
		}
	}
	navigation.active = true
	if now.Before(navigation.nextAt) {
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
		return position, false, true
	}
	distance := math.Hypot(dx, dy)
	// Match the bounded walking step used by the real-client movement probe.
	// Delayed scheduling never accumulates a teleport-sized catch-up step.
	if distance > 40 {
		dx *= 40 / distance
		dy *= 40 / distance
	}
	position.x += float32(dx)
	position.y += float32(dy)
	position.z, position.known = 0, 7
	return position, true, true
}

func (r *RobotVo) navigatePartyDungeonUnsafe(conn *net.UDPConn, now time.Time) bool {
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
	if !active {
		return false
	}
	// Leader state replay must not overwrite the independent approach. Its
	// latest position is already retained in the room's observed world.
	r.partyDungeonFollow = nil
	if !move {
		return true
	}
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
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
	}
	return true
}
