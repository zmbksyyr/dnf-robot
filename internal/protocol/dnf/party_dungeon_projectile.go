package dnf

import (
	"encoding/binary"
	"math"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

// The ordinary male-mage capture emits these four 24202 keyframes after
// state8, then destroys a missed projectile after 1000 ms. This is the packet
// execution path: the receiving client's monster controller owns collision.
var partyMageProjectileFrames = [...]struct {
	delay time.Duration
	x, z  int32
}{
	{168 * time.Millisecond, 65, 59},
	{252 * time.Millisecond, 60, 71},
	{168 * time.Millisecond, 55, 58},
	{104 * time.Millisecond, 65, 36},
}

type partyBasicProjectile struct {
	due, expires time.Time
	lifetime     time.Duration
	body         []byte
	reference    [4]byte
	objectIndex  uint32
	step         byte
	created      bool
	stopOnHit    bool
}

func buildPartyMageProjectile(owner, unique uint16, step byte, position partyDungeonPosition, right bool, key uint32) ([]byte, [4]byte) {
	frame := partyMageProjectileFrames[step]
	data := make([]byte, 59)
	ownerRef := partyDungeonPlayerReference(owner)
	copy(data[2:6], ownerRef[:])
	binary.LittleEndian.PutUint16(data[6:8], 0xffff)
	binary.LittleEndian.PutUint16(data[10:12], 1)
	binary.LittleEndian.PutUint32(data[12:16], 24202)
	reference := [4]byte{0, 0xf0, byte(unique), byte(unique >> 8)}
	copy(data[20:24], reference[:])
	dx := -frame.x
	if right {
		dx = frame.x
		data[25] = 1
	}
	binary.LittleEndian.PutUint32(data[26:30], uint32(int32(math.Round(float64(position.x)))+dx))
	binary.LittleEndian.PutUint32(data[30:34], uint32(int32(math.Round(float64(position.y)))+1))
	binary.LittleEndian.PutUint32(data[34:38], uint32(int32(math.Round(float64(position.z)))+frame.z))
	binary.LittleEndian.PutUint32(data[38:42], 12)
	// A stationary original caster writes three zero floats. The projectile's
	// own script supplies its travel; these are not the target's coordinates.
	data[57] = 1
	body, _ := buildPartyPassiveObjectBody(data, owner, key)
	return body, reference
}

func (r *RobotVo) queuePartyMageProjectileUnsafe(now time.Time, step byte, position, target partyDungeonPosition) {
	unique, key := r.nextPartyObjectIdentityUnsafe(now)
	body, reference := buildPartyMageProjectile(r.partySelfPeer.uniqueID, unique, step, position, target.x >= position.x, key)
	r.partyBasicProjectiles = append(r.partyBasicProjectiles, partyBasicProjectile{
		due: now.Add(partyMageProjectileFrames[step].delay), body: body, reference: reference, step: step,
		objectIndex: 24202, lifetime: time.Second, stopOnHit: true,
	})
}

func (r *RobotVo) nextPartyObjectIdentityUnsafe(now time.Time) (uint16, uint32) {
	if r.partyBasicObjectUnique == 0 {
		r.partyBasicObjectUnique = partySkillToken(r.UID, now) | 1
	}
	r.partyBasicObjectUnique++
	if r.partyBasicObjectUnique == 0 {
		r.partyBasicObjectUnique++
	}
	key := uint32(partySkillToken(r.UID, now))<<16 | uint32(r.partyBasicObjectUnique)
	return r.partyBasicObjectUnique, key
}

func (r *RobotVo) flushPartyBasicProjectilesUnsafe(conn *net.UDPConn, now time.Time) {
	if !shared.PartySkillEnabled() || r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonLastAt) > partyDungeonActivityTimeout {
		r.partyBasicProjectiles = nil
		return
	}
	if r.partyDungeonLoading || len(r.partyBasicProjectiles) == 0 {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	remaining := r.partyBasicProjectiles[:0]
	for _, object := range r.partyBasicProjectiles {
		if !object.created {
			if now.Before(object.due) || r.partyReliablePurposePendingUnsafe(peer.slot, "skill-cast") {
				remaining = append(remaining, object)
				continue
			}
			if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{object.body}, "skill-object", now); err != nil {
				remaining = append(remaining, object)
				continue
			}
			object.created, object.expires = true, now.Add(object.lifetime)
			r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", object.body)
			foundationlog.Robotf("[PARTY_DUNGEON_PROJECTILE] uid=%d object=%d ref=%x step=%d\n", r.UID, object.objectIndex, object.reference, object.step)
		}
		if now.Before(object.expires) {
			remaining = append(remaining, object)
			continue
		}
		body := buildPartyObjectDestroy(object.reference)
		if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{body}, "attack-object-end", now); err != nil {
			remaining = append(remaining, object)
			continue
		}
		r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", body)
	}
	r.partyBasicProjectiles = remaining
}

func buildPartyObjectDestroy(reference [4]byte) []byte {
	body := make([]byte, 11)
	body[0], body[1] = 2, 23
	copy(body[7:], reference[:])
	checksum := partyPayloadChecksum(body[7:])
	copy(body[3:7], checksum[:])
	for i := 7; i < len(body); i++ {
		body[i] ^= 0x7e
	}
	return body
}

func (r *RobotVo) observePartyProjectileHitUnsafe(body []byte, now time.Time) {
	if len(r.partyBasicProjectiles) == 0 || len(body) < 11 {
		return
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	if command != 19 {
		return
	}
	plain := make([]byte, len(body)-7)
	for i, value := range body[7:] {
		plain[i] = value ^ 0x7e
	}
	checksum := partyPayloadChecksum(plain)
	if body[3] != checksum[0] {
		return
	}
	for i := range r.partyBasicProjectiles {
		object := &r.partyBasicProjectiles[i]
		if object.created && object.stopOnHit && string(plain[:4]) == string(object.reference[:]) {
			object.expires = now
		}
	}
}
