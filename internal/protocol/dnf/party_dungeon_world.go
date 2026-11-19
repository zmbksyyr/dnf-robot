package dnf

import (
	"encoding/binary"
	"math"
	"time"
)

type partyDungeonActor struct {
	position partyDungeonPosition
	seenAt   time.Time
	hp       int32
	hpKnown  bool
	dead     bool
}

type partyDungeonWorld struct {
	actors     map[[4]byte]partyDungeonActor
	target     [4]byte
	navigation partyDungeonNavigation
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

func (r *RobotVo) observePartyDungeonFrameUnsafe(frame []byte, peer partyIPPeer, now time.Time) {
	if r.partyDungeonEnteredAt.IsZero() || !r.shouldFollowPartyPeerUnsafe(peer) {
		return
	}
	bodies, ok := partyDungeonFrameBodies(frame)
	if !ok {
		return
	}
	for _, body := range bodies {
		r.partyDungeonWorld.observe(body, now)
	}
}

func (world *partyDungeonWorld) observe(body []byte, now time.Time) {
	if entries, ok := decodePartyDungeonMotion(body); ok {
		if world.actors == nil {
			world.actors = make(map[[4]byte]partyDungeonActor)
		}
		for _, entry := range entries {
			if entry.entity[0] != 0x11 || (entry.entity[1] != 1 && entry.entity[1] != 2) {
				continue
			}
			actor := world.actors[entry.entity]
			actor.position = actor.position.apply(entry)
			actor.seenAt = now
			world.actors[entry.entity] = actor
		}
		return
	}
	if len(body) < 11 || (body[0] != 1 && body[0] != 2) {
		return
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	if command != 6 && command != 12 && command != 23 {
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
	if command == 6 && len(payload) != 6 || command == 12 && len(payload) != 16 || command == 23 && len(payload) != 4 {
		return
	}
	var ref [4]byte
	copy(ref[:], payload)
	if ref[0] != 0x11 || (ref[1] != 1 && ref[1] != 2) {
		return
	}
	if world.actors == nil {
		world.actors = make(map[[4]byte]partyDungeonActor)
	}
	actor := world.actors[ref]
	actor.seenAt = now
	if command == 12 {
		actor.hp = int32(binary.LittleEndian.Uint32(payload[4:]))
		actor.hpKnown = true
		actor.dead = actor.hp <= 0 || partyDungeonMonster(ref) && actor.dead
	} else if command == 23 {
		actor.dead = true
	}
	world.actors[ref] = actor
	if actor.dead && world.target == ref {
		world.target = [4]byte{}
	}
}

func (world *partyDungeonWorld) nearestMonster(position partyDungeonPosition, now time.Time) ([4]byte, partyDungeonPosition, bool) {
	if position.known&3 != 3 {
		return [4]byte{}, partyDungeonPosition{}, false
	}
	// Keep a live target while approaching it. Re-evaluating nearby monsters
	// every movement tick made the Robot repeatedly cross their attack line.
	if actor, ok := world.actors[world.target]; ok && partyDungeonMonster(world.target) && !actor.dead && actor.position.known&3 == 3 && now.Sub(actor.seenAt) <= 3*time.Second {
		return world.target, actor.position, true
	}
	var selected [4]byte
	var point partyDungeonPosition
	best := math.Inf(1)
	for ref, actor := range world.actors {
		if !partyDungeonMonster(ref) || actor.dead || actor.position.known&3 != 3 || now.Sub(actor.seenAt) > 3*time.Second {
			continue
		}
		distance := math.Hypot(float64(actor.position.x-position.x), 2*float64(actor.position.y-position.y))
		if distance < best || distance == best && binary.LittleEndian.Uint32(ref[:]) < binary.LittleEndian.Uint32(selected[:]) {
			selected, point, best = ref, actor.position, distance
		}
	}
	world.target = selected
	return selected, point, best < math.Inf(1)
}
