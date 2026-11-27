package dnf

import (
	"encoding/binary"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
)

// USE_SKILL carries the resulting MP, not the amount consumed. Put it before
// SET_STATE in the same reliable frame so retransmission cannot pay twice.
func buildPartyUseSkillBody(unique uint16, skill, level byte, mp int32) []byte {
	payload := make([]byte, 11)
	ref := partyDungeonPlayerReference(unique)
	copy(payload, ref[:])
	payload[4], payload[5] = skill, level
	binary.LittleEndian.PutUint32(payload[6:10], uint32(mp))
	body := make([]byte, 7+len(payload))
	body[0], body[1] = 2, 25
	checksum := partyPayloadChecksum(payload)
	copy(body[3:7], checksum[:])
	for i, value := range payload {
		body[7+i] = value ^ 0x7e
	}
	return body
}

func (r *RobotVo) sendPartySkillCastUnsafe(conn *net.UDPConn, now time.Time, candidate partySkillCandidate) bool {
	r.seedPartyResourcesUnsafe()
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[self]
	previousMP := actor.mp
	var use []byte
	var paidMP int32
	if actor.mpKnown && len(candidate.baseMPByLevel) > 0 {
		level := min(max(candidate.level, 1), len(candidate.baseMPByLevel))
		cost := int32(candidate.baseMPByLevel[level-1])
		if cost > 0 && actor.mp < cost {
			return false
		}
		paidMP = actor.mp - cost
		use = buildPartyUseSkillBody(r.partySelfPeer.uniqueID, candidate.skillIndex, byte(candidate.level), paidMP)
	}
	if !r.sendPartySkillRecordsUnsafe(conn, now, candidate.state, candidate.stateData, "CAST", use) {
		return false
	}
	if use != nil {
		// Apply the exact value sent only after the transport accepts the frame.
		actor.mp = paidMP
		actor.lastSkill, actor.skillLevel, actor.lastSkillAt = candidate.skillIndex, byte(candidate.level), now
		r.partyDungeonWorld.actors[self] = actor
		r.rememberPartyResourcesUnsafe()
		foundationlog.Robotf("[PARTY_SKILL_MP] uid=%d cid=%d skill=%d level=%d before=%d after=%d\n", r.UID, r.CID, candidate.skillIndex, candidate.level, previousMP, actor.mp)
	}
	return true
}
