package dnf

import (
	"encoding/binary"
	"math"
	"time"
)

// The original native Earth Crack Wave Sword sends state24/[20], then creates
// 20001 after 119 ms. Its skill level is an explicit creation parameter; the
// script-data length is zero. This wave pierces, so a HIT must not destroy it.
func buildPartySwordWave(owner, unique uint16, level int, position partyDungeonPosition, right bool, key uint32) ([]byte, [4]byte) {
	data := make([]byte, 47)
	ownerRef := partyDungeonPlayerReference(owner)
	copy(data[2:6], ownerRef[:])
	binary.LittleEndian.PutUint16(data[6:8], 0xffff)
	binary.LittleEndian.PutUint16(data[10:12], 1)
	binary.LittleEndian.PutUint32(data[12:16], 20001)
	binary.LittleEndian.PutUint32(data[16:20], uint32(level))
	reference := [4]byte{0, 0xf0, byte(unique), byte(unique >> 8)}
	copy(data[20:24], reference[:])
	dx := int32(-10)
	if right {
		dx, data[25] = 10, 1
	}
	binary.LittleEndian.PutUint32(data[26:30], uint32(int32(math.Round(float64(position.x)))+dx))
	binary.LittleEndian.PutUint32(data[30:34], uint32(int32(math.Round(float64(position.y)))))
	data[45] = 1
	body, _ := buildPartyPassiveObjectBody(data, owner, key)
	return body, reference
}

func (r *RobotVo) queuePartySkillObjectUnsafe(candidate partySkillCandidate, now time.Time) {
	if r.partySkillJob != 0 || candidate.skillIndex != 20 || candidate.state != 24 {
		return
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	position := r.partyDungeonWorld.actors[self].position
	_, target, ok := r.partyDungeonWorld.nearestTarget(self, position)
	if !ok {
		return
	}
	unique, key := r.nextPartyObjectIdentityUnsafe(now)
	body, reference := buildPartySwordWave(r.partySelfPeer.uniqueID, unique, candidate.level, position, target.x >= position.x, key)
	r.partyBasicProjectiles = append(r.partyBasicProjectiles, partyBasicProjectile{
		due: now.Add(119 * time.Millisecond), lifetime: 350 * time.Millisecond,
		body: body, reference: reference, objectIndex: 20001,
	})
}
