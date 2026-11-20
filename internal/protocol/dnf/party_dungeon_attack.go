package dnf

import (
	"math"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
)

const partyBasicAttackInterval = 1200 * time.Millisecond

// Swordman state 8 with vector [0] is the first normal swing observed from a
// real player. The receiving client uses this actor's own attack information;
// no damage value or copied passive object is sent. Other jobs and the newer
// state envelope need their own client verification before enabling them.
func (r *RobotVo) tryPartyBasicAttackUnsafe(conn *net.UDPConn, now time.Time) bool {
	if !r.partySkillLoaded || r.partySkillJob != 0 || !r.partyDungeonLegacySkills || now.Before(r.partyBasicAttackNextAt) {
		return false
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) || r.partyReliablePurposePendingUnsafe(peer.slot, "skill-") {
		return false
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	position := r.partyDungeonWorld.actors[self].position
	_, target, ok := r.partyDungeonWorld.nearestMonster(position, now)
	if !ok || math.Abs(float64(target.x-position.x)) > 100 || math.Abs(float64(target.y-position.y)) > 25 {
		return false
	}
	// Face the selected enemy without changing position, then start the swing
	// in the same ordered reliable frame. This also handles a monster crossing
	// behind an actor that has already stopped walking into attack range.
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
	if target.x >= position.x {
		entry.flags[2] |= 0x20
	}
	for i, value := range []float32{position.x, position.y} {
		entry.modes &^= 3 << uint((9+i)*2)
		entry.values[9+i] = math.Float32bits(value)
	}
	face, ok := buildPartyDungeonMotion([]partyDungeonMotion{entry})
	if !ok {
		return false
	}
	attack := buildPartyLegacySkillStateBody(r.partySelfPeer.uniqueID, 8, []byte{0}, partySkillToken(r.UID, now))
	if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{face, attack}, "skill-cast", now); err != nil {
		return false
	}
	r.partyBasicAttackNextAt = now.Add(partyBasicAttackInterval)
	r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
	r.partyDungeonFollow = nil
	foundationlog.Robotf("[PARTY_DUNGEON_ATTACK] uid=%d job=%d state=8 target=%x recover_in=%s\n", r.UID, r.partySkillJob, r.partyDungeonWorld.target, partySkillRecoverDelay)
	return true
}
