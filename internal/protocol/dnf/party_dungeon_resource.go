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
	cost, known := r.partySkillMPCostUnsafe(candidate)
	if !known || r.characterBaseInfo.known && !actor.mpKnown && cost > 0 {
		return false
	}
	if actor.mpKnown {
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

func buildPartyResourceBody(command byte, unique uint16, value int32) []byte {
	size := 8
	if command == 12 {
		size = 16
	}
	payload := make([]byte, size)
	ref := partyDungeonPlayerReference(unique)
	copy(payload, ref[:])
	binary.LittleEndian.PutUint32(payload[4:], uint32(value))
	if command == 12 {
		binary.LittleEndian.PutUint32(payload[12:], 0xffffffff)
	}
	body := make([]byte, 7+size)
	body[0], body[1] = 2, command
	checksum := partyPayloadChecksum(payload)
	copy(body[3:7], checksum[:])
	for i, value := range payload {
		body[7+i] = value ^ 0x7e
	}
	return body
}

func (r *RobotVo) flushPartyResourcesUnsafe(conn *net.UDPConn, now time.Time) {
	r.initializeCharacterResourcesUnsafe()
	pool := &r.characterResources
	if r.partyDungeonEnteredAt.IsZero() || r.partyDungeonLoading {
		return
	}
	r.seedPartyResourcesUnsafe()
	// HP_DAMAGE/SET_HP and SET_MP/USE_SKILL are the authoritative resource
	// transitions. Do not synthesize recovery or damage while the peer is silent.
	if !pool.syncPending || (!pool.hpObserved && !pool.mpObserved) {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) {
		return
	}
	var records [][]byte
	if pool.hpObserved {
		records = append(records, buildPartyResourceBody(12, r.partySelfPeer.uniqueID, pool.hp))
	}
	if pool.mpObserved {
		records = append(records, buildPartyResourceBody(13, r.partySelfPeer.uniqueID, pool.mp))
	}
	if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, records, "resources", now); err != nil {
		return
	}
	pool.syncPending = false
	for _, body := range records {
		r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", body)
	}
}
