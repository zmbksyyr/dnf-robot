package dnf

import (
	"encoding/binary"
	"math"
	"time"

	"robot/internal/shared"
)

type partyDungeonActor struct {
	position     partyDungeonPosition
	room         [2]byte
	seenAt       time.Time
	hp           int32
	hpKnown      bool
	dead         bool
	team         uint16
	teamKnown    bool
	objective    bool
	lastDamageAt time.Time
	lastAttackAt time.Time
}

type partyDungeonWorld struct {
	actors         map[[4]byte]partyDungeonActor
	entryFrom      [2]byte
	entryFromKnown bool
	target         [4]byte
	targetAttackAt time.Time
	navigation     partyDungeonNavigation
	objectives     []shared.DungeonObject
}

func partyDungeonFrameBodies(frame []byte) ([][]byte, bool) {
	if len(frame) < 9 || len(frame) != 9+int(binary.LittleEndian.Uint16(frame[5:7])) {
		return nil, false
	}
	payload := frame[9:]
	if frame[0] == 2 {
		return [][]byte{payload}, true
	}
	if frame[0] != 1 {
		return nil, false
	}
	var bodies [][]byte
	for len(payload) >= 2 {
		size := int(binary.LittleEndian.Uint16(payload))
		payload = payload[2:]
		if size == 0 || size > len(payload) {
			return nil, false
		}
		bodies = append(bodies, payload[:size])
		payload = payload[size:]
	}
	return bodies, len(payload) == 0
}

func partyDungeonPlayerReference(unique uint16) (ref [4]byte) {
	ref[0], ref[1] = 0x11, 1
	binary.LittleEndian.PutUint16(ref[2:], unique)
	return
}

func partyDungeonMonster(ref [4]byte) bool {
	return ref[0] == 0x11 && ref[1] == 2
}

func partyDungeonMapObject(ref [4]byte) bool {
	return ref[0] == 0x21 && ref[1] == 0
}

func (world *partyDungeonWorld) mapObjective(position partyDungeonPosition) bool {
	if position.known != 7 {
		return false
	}
	for _, object := range world.objectives {
		if position.x == float32(object.X) && position.y == float32(object.Y) && position.z == float32(object.Z) {
			return true
		}
	}
	return false
}

func (r *RobotVo) observePartyDungeonFrameUnsafe(frame []byte, peer partyIPPeer, now time.Time) {
	if r.partyDungeonEnteredAt.IsZero() || !r.shouldFollowPartyPeerUnsafe(peer) {
		return
	}
	// Walk the bodies in place: this runs for every party frame and the slice of
	// bodies was pure per-frame garbage.
	if len(frame) < 9 || len(frame) != 9+int(binary.LittleEndian.Uint16(frame[5:7])) {
		return
	}
	payload := frame[9:]
	if frame[0] == 2 {
		r.recordPartyDungeonBodyUnsafe(peer.accID, "RX", payload)
		r.observePartyProjectileHitUnsafe(payload, now)
		r.partyDungeonWorld.observe(payload, now)
		return
	}
	if frame[0] != 1 {
		return
	}
	for len(payload) >= 2 {
		size := int(binary.LittleEndian.Uint16(payload))
		payload = payload[2:]
		if size == 0 || size > len(payload) {
			return
		}
		r.recordPartyDungeonBodyUnsafe(peer.accID, "RX", payload[:size])
		r.observePartyProjectileHitUnsafe(payload[:size], now)
		r.partyDungeonWorld.observe(payload[:size], now)
		payload = payload[size:]
	}
}

func (world *partyDungeonWorld) observe(body []byte, now time.Time) {
	if entries, ok := decodePartyDungeonMotion(body); ok {
		if world.actors == nil {
			world.actors = make(map[[4]byte]partyDungeonActor)
		}
		for _, entry := range entries {
			if !partyDungeonMapObject(entry.entity) && (entry.entity[0] != 0x11 || (entry.entity[1] != 1 && entry.entity[1] != 2)) {
				continue
			}
			actor := world.actors[entry.entity]
			room := [2]byte{entry.flags[0], entry.flags[1]}
			if !actor.seenAt.IsZero() && actor.room != room {
				// Retain fields belong to one room's coordinate system. A late
				// delta from another room cannot inherit this room's coordinates.
				actor.position = partyDungeonPosition{}
			}
			actor.position = actor.position.apply(entry)
			actor.room = room
			actor.objective = actor.objective || partyDungeonMapObject(entry.entity) && world.mapObjective(actor.position)
			actor.seenAt = now
			world.actors[entry.entity] = actor
		}
		return
	}
	if len(body) < 11 || (body[0] != 1 && body[0] != 2) {
		return
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	if command == partyDungeonEnvelopeCommand {
		if inner, payload, ok := decodePartyDungeonEnvelope(body); ok {
			world.observeActorEvent(inner, payload, now)
		}
		return
	}
	if command != 6 && command != 12 && command != 20 && command != 23 && command != 54 {
		return
	}
	payload := make([]byte, len(body)-7)
	for i, value := range body[7:] {
		payload[i] = value ^ 0x7e
	}
	checksum := partyPayloadChecksum(payload)
	if body[3] != checksum[0] || body[0] == 1 && string(body[3:7]) != string(checksum[:]) {
		return
	}
	world.observeActorEvent(command, payload, now)
}

func (world *partyDungeonWorld) observeActorEvent(command uint16, payload []byte, now time.Time) {
	if command != 6 && command != 12 && command != 20 && command != 23 && command != 54 {
		return
	}
	if (command == 6 || command == 54) && len(payload) != 6 || command == 12 && len(payload) != 16 || command == 20 && len(payload) != 44 || command == 23 && len(payload) != 4 {
		return
	}
	var ref [4]byte
	copy(ref[:], payload)
	if command == 20 {
		// HP_DAMAGE identifies the attacker first, then the victim. Its current
		// HP follows a float, five shorts and two damage-related integers.
		// Read the peer's resulting HP; do not subtract the reported damage.
		copy(ref[:], payload[4:8])
	}
	if !partyDungeonMapObject(ref) && (ref[0] != 0x11 || (ref[1] != 1 && ref[1] != 2)) {
		return
	}
	if world.actors == nil {
		world.actors = make(map[[4]byte]partyDungeonActor)
	}
	actor := world.actors[ref]
	actor.seenAt = now
	if command == 12 || command == 20 {
		hpOffset := 4
		if command == 20 {
			hpOffset = 30
		}
		hp := int32(binary.LittleEndian.Uint32(payload[hpOffset:]))
		if !actor.hpKnown || hp < actor.hp {
			actor.lastDamageAt = now
		}
		actor.hp = hp
		actor.hpKnown = true
		actor.dead = actor.hp <= 0 || (partyDungeonMonster(ref) || actor.objective) && actor.dead
	} else if command == 23 {
		actor.dead = true
	} else if command == 54 {
		// Original SET_TEAM reads group, unique and team as three words,
		// then calls virtual+0x228. Its getter (+0x22c) feeds the ordinary
		// relation comparison in sq_FindEnemyTarget's 0x872c60 predicate.
		actor.team = binary.LittleEndian.Uint16(payload[4:])
		actor.teamKnown = true
	}
	world.actors[ref] = actor
	if actor.dead && world.target == ref {
		world.target = [4]byte{}
		world.targetAttackAt = time.Time{}
	}
}

func (world *partyDungeonWorld) targetable(self, ref [4]byte, actor partyDungeonActor) bool {
	if !(partyDungeonMonster(ref) || actor.objective) || actor.dead || actor.position.known&3 != 3 {
		return false
	}
	// This is a same-team exclusion, not the complete native enemy predicate:
	// native status effects can override relations and are not observed here.
	// Only compare received teams; kind and party slot do not establish a team.
	// Authored map objectives retain their separate selection rules.
	owner := world.actors[self]
	// Native sq_FindEnemyTarget traverses the owner's current scene vector
	// (virtual+0x64, then scene+0xb0/+0xb4). A delayed motion record can put
	// another room's actors back into our protocol view after a room reset.
	// Compare the received room grid whenever the owner's position is known;
	// (0,0) is a valid room, not an unknown sentinel.
	if owner.position.known&3 == 3 && owner.room != actor.room {
		return false
	}
	return !partyDungeonMonster(ref) || !owner.teamKnown || !actor.teamKnown || owner.team != actor.team
}

// combatRoom keeps an attack in the owner's observed coordinate system. Peer
// motion can announce a different room before the server's loading/reset packet;
// wait for the transition instead of stamping old coordinates with the peer room.
func (world *partyDungeonWorld) combatRoom(self, leader [4]byte) ([2]byte, bool) {
	owner := world.actors[self]
	peer := world.actors[leader]
	if owner.position.known&3 != 3 || peer.position.known&3 == 3 && peer.room != owner.room {
		return [2]byte{}, false
	}
	return owner.room, true
}

func (world *partyDungeonWorld) nearestTarget(self [4]byte, position partyDungeonPosition) ([4]byte, partyDungeonPosition, bool) {
	if position.known&3 != 3 {
		return [4]byte{}, partyDungeonPosition{}, false
	}
	// Motion is a delta stream, not an actor heartbeat: a living monster can
	// go several seconds without an update. Keep it until a death/destroy or
	// room reset; timing out its last position made the Robot abandon enemies.
	// Keep the chosen target while approaching instead of crossing between them.
	if actor, ok := world.actors[world.target]; ok && world.targetable(self, world.target, actor) {
		return world.target, actor.position, true
	}
	var selected [4]byte
	var point partyDungeonPosition
	best := math.Inf(1)
	for ref, actor := range world.actors {
		if !world.targetable(self, ref, actor) {
			continue
		}
		distance := math.Hypot(float64(actor.position.x-position.x), 2*float64(actor.position.y-position.y))
		if distance < best || distance == best && binary.LittleEndian.Uint32(ref[:]) < binary.LittleEndian.Uint32(selected[:]) {
			selected, point, best = ref, actor.position, distance
		}
	}
	world.target = selected
	world.targetAttackAt = time.Time{}
	return selected, point, best < math.Inf(1)
}

// Record attempted attacks only after their frame was sent. Motion silence is
// not evidence of death or failed combat, and approaching a target is not an
// attack attempt.
func (world *partyDungeonWorld) noteTargetAttack(ref [4]byte, now time.Time) {
	if ref != world.target {
		return
	}
	actor, ok := world.actors[ref]
	if !ok || actor.dead {
		return
	}
	actor.lastAttackAt = now
	world.actors[ref] = actor
	if world.targetAttackAt.IsZero() {
		world.targetAttackAt = now
	}
}

// Give other observed enemies a turn after sustained attacks without an HP
// decrease. Keep every actor and its HP: this changes combat priority only.
// Prefer the least recently attacked alternative so several unresponsive
// targets cannot continually hide the remaining enemies.
func (world *partyDungeonWorld) reconsiderTarget(self [4]byte, position partyDungeonPosition, now time.Time) {
	const progressTimeout = 15 * time.Second
	if position.known&3 != 3 || world.targetAttackAt.IsZero() {
		return
	}
	current := world.actors[world.target]
	progressAt := world.targetAttackAt
	if current.lastDamageAt.After(progressAt) {
		progressAt = current.lastDamageAt
	}
	if now.Sub(progressAt) < progressTimeout || now.Sub(current.lastAttackAt) > 3*time.Second {
		return
	}
	var selected [4]byte
	var lastAttackAt time.Time
	best := math.Inf(1)
	for ref, actor := range world.actors {
		if ref == world.target || !world.targetable(self, ref, actor) {
			continue
		}
		distance := math.Hypot(float64(actor.position.x-position.x), 2*float64(actor.position.y-position.y))
		if best == math.Inf(1) || actor.lastAttackAt.Before(lastAttackAt) || actor.lastAttackAt.Equal(lastAttackAt) &&
			(distance < best || distance == best && binary.LittleEndian.Uint32(ref[:]) < binary.LittleEndian.Uint32(selected[:])) {
			selected, lastAttackAt, best = ref, actor.lastAttackAt, distance
		}
	}
	if best < math.Inf(1) {
		world.target = selected
		world.targetAttackAt = time.Time{}
	}
}
