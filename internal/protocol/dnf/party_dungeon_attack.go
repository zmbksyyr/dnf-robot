package dnf

import (
	"math"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

const (
	partyBasicAttackInterval = 1200 * time.Millisecond
	// A real male-mage four-tap capture used approximately 312 ms between
	// stages. Allow a little more time; this is not an animation speed formula.
	partyBasicComboInterval = 350 * time.Millisecond
	partySwordComboInterval = 600 * time.Millisecond
)

// Original captures establish three swordman/female-mage swings and four
// male-mage swings. Male gunner still uses its observed first attack.
func (r *RobotVo) tryPartyBasicAttackUnsafe(conn *net.UDPConn, now time.Time) bool {
	if !shared.PartySkillEnabled() || !r.partySkillLoaded || (r.partySkillJob != 0 && r.partySkillJob != 2 && r.partySkillJob != 3 && r.partySkillJob != 8) || now.Before(r.partyBasicAttackNextAt) {
		return false
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) || r.partyReliablePurposePendingUnsafe(peer.slot, "skill-") {
		return false
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	if !r.partyDungeonWorld.playerCanAct(r.partySelfPeer.uniqueID) {
		return false
	}
	room, roomKnown := r.partyDungeonWorld.combatRoom(self, partyDungeonPlayerReference(peer.uniqueID))
	if !roomKnown {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return false
	}
	position := r.partyDungeonWorld.actors[self].position
	targetID, target, ok := r.partyDungeonWorld.nearestTarget(self, position)
	if r.partyBasicAttackStep != 0 && (!ok || targetID != r.partyBasicAttackTarget) {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return false
	}
	if !ok || !partyCloseAttackInRange(position, target) {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return false
	}
	// Face the selected enemy without changing position, then start the swing
	// in the same ordered reliable frame. This also handles a monster crossing
	// behind an actor that has already stopped walking into attack range.
	face, ok := buildPartyAttackFacing(r.partySelfPeer.uniqueID, position, target, room)
	if !ok {
		return false
	}
	step := r.partyBasicAttackStep
	attack := buildPartySkillStateBody(r.partySelfPeer.uniqueID, 8, []byte{step}, partySkillToken(r.UID, now))
	if r.partyDungeonLegacySkills {
		attack = buildPartyLegacySkillStateBody(r.partySelfPeer.uniqueID, 8, []byte{step}, partySkillToken(r.UID, now))
	}
	if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{face, attack}, "skill-cast", now); err != nil {
		return false
	}
	if r.partySkillJob == 8 {
		r.queuePartyMageProjectileUnsafe(now, step, position, target)
	}
	r.partyDungeonWorld.noteTargetAttack(targetID, now)
	r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", attack)
	r.partyBasicAttackNextAt = now.Add(partyBasicAttackInterval)
	r.partyBasicAttackStep = 0
	r.partyBasicAttackTarget = [4]byte{}
	lastStep := byte(0)
	if r.partySkillJob == 0 || r.partySkillJob == 3 {
		lastStep = 2
	}
	if r.partySkillJob == 8 {
		lastStep = 3
	}
	if step < lastStep {
		r.partyBasicAttackStep = step + 1
		r.partyBasicAttackTarget = targetID
		r.partyBasicAttackNextAt = now.Add(partyBasicComboInterval)
		if r.partySkillJob == 0 {
			// Original swordman state8/[0,1,2] advances after 545/540 ms.
			r.partyBasicAttackNextAt = now.Add(partySwordComboInterval)
		}
		if r.partySkillJob == 3 {
			// The original client sample advances after about 376/625 ms.
			// Leave a margin without claiming an animation-speed formula.
			delay := 450 * time.Millisecond
			if step == 1 {
				delay = 700 * time.Millisecond
			}
			r.partyBasicAttackNextAt = now.Add(delay)
		}
	}
	r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
	r.partyDungeonFollow = nil
	foundationlog.Robotf("[PARTY_DUNGEON_ATTACK] uid=%d job=%d state=8 step=%d target=%x recover_in=%s\n", r.UID, r.partySkillJob, step, targetID, partySkillRecoverDelay)
	return true
}

// The approach policy shared by normal swings and the native uppercut skills.
// It is a casting distance, not a replacement for the client's collision box.
func partyCloseAttackInRange(position, target partyDungeonPosition) bool {
	return position.known&3 == 3 && target.known&3 == 3 &&
		math.Abs(float64(target.x-position.x)) <= 100 && math.Abs(float64(target.y-position.y)) <= 25
}

func buildPartyAttackFacing(uniqueID uint16, position, target partyDungeonPosition, room [2]byte) ([]byte, bool) {
	self := partyDungeonPlayerReference(uniqueID)
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0x415555, flags: [3]byte{room[0], room[1], 0x10}}
	if target.x >= position.x {
		entry.flags[2] |= 0x20
	}
	for i, value := range []float32{position.x, position.y} {
		entry.modes &^= 3 << uint((9+i)*2)
		entry.values[9+i] = math.Float32bits(value)
	}
	return buildPartyDungeonMotion([]partyDungeonMotion{entry})
}
