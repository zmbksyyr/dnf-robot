package dnf

import (
	"math"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
)

const (
	partyBasicAttackInterval = 1200 * time.Millisecond
	// A real male-mage four-tap capture used approximately 312 ms between
	// stages. Allow a little more time; this is not an animation speed formula.
	partyBasicComboInterval = 350 * time.Millisecond
)

// Swordman uses its verified first swing. Male mage uses the four state-8
// vectors [0], [1], [2], [3] captured from a real player and corresponding to
// its four PVF attack motions. Other jobs and envelopes need verification.
func (r *RobotVo) tryPartyBasicAttackUnsafe(conn *net.UDPConn, now time.Time) bool {
	if !r.partySkillLoaded || (r.partySkillJob != 0 && r.partySkillJob != 8) || !r.partyDungeonLegacySkills || now.Before(r.partyBasicAttackNextAt) {
		return false
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) || r.partyReliablePurposePendingUnsafe(peer.slot, "skill-") {
		return false
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	position := r.partyDungeonWorld.actors[self].position
	targetID, target, ok := r.partyDungeonWorld.nearestMonster(position, now)
	if r.partyBasicAttackStep != 0 && (!ok || targetID != r.partyBasicAttackTarget) {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return false
	}
	if !ok || math.Abs(float64(target.x-position.x)) > 100 || math.Abs(float64(target.y-position.y)) > 25 {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return false
	}
	// Face the selected enemy without changing position, then start the swing
	// in the same ordered reliable frame. This also handles a monster crossing
	// behind an actor that has already stopped walking into attack range.
	face, ok := buildPartyAttackFacing(r.partySelfPeer.uniqueID, position, target)
	if !ok {
		return false
	}
	step := r.partyBasicAttackStep
	attack := buildPartyLegacySkillStateBody(r.partySelfPeer.uniqueID, 8, []byte{step}, partySkillToken(r.UID, now))
	if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{face, attack}, "skill-cast", now); err != nil {
		return false
	}
	r.partyBasicAttackNextAt = now.Add(partyBasicAttackInterval)
	r.partyBasicAttackStep = 0
	r.partyBasicAttackTarget = [4]byte{}
	if r.partySkillJob == 8 && step < 3 {
		r.partyBasicAttackStep = step + 1
		r.partyBasicAttackTarget = targetID
		r.partyBasicAttackNextAt = now.Add(partyBasicComboInterval)
	}
	r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
	r.partyDungeonFollow = nil
	foundationlog.Robotf("[PARTY_DUNGEON_ATTACK] uid=%d job=%d state=8 step=%d target=%x recover_in=%s\n", r.UID, r.partySkillJob, step, targetID, partySkillRecoverDelay)
	return true
}

func buildPartyAttackFacing(uniqueID uint16, position, target partyDungeonPosition) ([]byte, bool) {
	self := partyDungeonPlayerReference(uniqueID)
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
	if target.x >= position.x {
		entry.flags[2] |= 0x20
	}
	for i, value := range []float32{position.x, position.y} {
		entry.modes &^= 3 << uint((9+i)*2)
		entry.values[9+i] = math.Float32bits(value)
	}
	return buildPartyDungeonMotion([]partyDungeonMotion{entry})
}
