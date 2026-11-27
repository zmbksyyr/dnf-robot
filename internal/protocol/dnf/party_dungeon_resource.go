package dnf

import (
	"encoding/binary"
	"math"
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
	if actor.mpKnown {
		cost := candidate.simulatedMPCost(r.characterResources.maxMP)
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

func (c partySkillCandidate) simulatedMPCost(maxMP int32) int32 {
	cost := int32(0)
	if len(c.baseMPByLevel) > 0 {
		level := min(max(c.level, 1), len(c.baseMPByLevel))
		cost = max(0, int32(c.baseMPByLevel[level-1]))
	}
	if maxMP > 0 {
		cost = max(cost, max(1, maxMP/20))
	}
	return cost
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

// The peer broadcasts monster attacks but does not calculate hits for a remote
// PC. Nearby received attacks drive deliberately approximate HP attrition.
func (r *RobotVo) simulatePartyHurtUnsafe(now time.Time) {
	pool := &r.characterResources
	if !pool.known || now.Sub(pool.lastHurtAt) < 700*time.Millisecond {
		return
	}
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[self]
	if actor.position.known&3 != 3 || actor.dead {
		return
	}
	for ref, enemy := range r.partyDungeonWorld.actors {
		attackAt := enemy.incomingAttackAt
		if actor.lastHitFrom == ref && actor.lastHitAt.After(attackAt) {
			attackAt = actor.lastHitAt
		}
		if !partyDungeonMonster(ref) || enemy.dead || enemy.room != actor.room || enemy.position.known&3 != 3 ||
			!attackAt.After(pool.lastHurtAt) || now.Sub(attackAt) > time.Second ||
			math.Abs(float64(enemy.position.x-actor.position.x)) > 160 || math.Abs(float64(enemy.position.y-actor.position.y)) > 65 {
			continue
		}
		before := actor.hp
		damage := max(1, pool.maxHP*(5+int32((uint64(ref[2])+uint64(attackAt.UnixMilli()/100))%5))/100)
		// This simulation models attrition and recovery, not death/revival.
		actor.hp = max(1, actor.hp-damage)
		actor.hpKnown, actor.lastHitFrom, actor.lastHitAt = true, ref, now
		r.partyDungeonWorld.actors[self] = actor
		pool.hp, pool.lastHurtAt, pool.syncPending = actor.hp, now, true
		foundationlog.Robotf("[PARTY_SIM_HURT] uid=%d source=%x before=%d after=%d\n", r.UID, ref, before, actor.hp)
		return
	}
}

func (r *RobotVo) flushPartyResourcesUnsafe(conn *net.UDPConn, now time.Time) {
	r.initializeCharacterResourcesUnsafe()
	pool := &r.characterResources
	if !pool.known {
		return
	}
	if pool.recoveredAt.IsZero() {
		pool.recoveredAt = now
	}
	if seconds := int32(now.Sub(pool.recoveredAt) / time.Second); seconds > 0 {
		pool.recoveredAt = pool.recoveredAt.Add(time.Duration(seconds) * time.Second)
		beforeHP, beforeMP := pool.hp, pool.mp
		pool.hp = min(pool.maxHP, pool.hp+seconds*max(1, pool.maxHP/50))
		pool.mp = min(pool.maxMP, pool.mp+seconds*max(1, pool.maxMP/100))
		if pool.hp != beforeHP || pool.mp != beforeMP {
			pool.syncPending = true
			foundationlog.Robotf("[PARTY_SIM_RECOVER] uid=%d hp=%d->%d mp=%d->%d\n", r.UID, beforeHP, pool.hp, beforeMP, pool.mp)
		}
	}
	if r.partyDungeonEnteredAt.IsZero() || r.partyDungeonLoading {
		return
	}
	r.seedPartyResourcesUnsafe()
	self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[self]
	actor.hp, actor.mp = pool.hp, pool.mp
	r.partyDungeonWorld.actors[self] = actor
	r.simulatePartyHurtUnsafe(now)
	if !pool.syncPending {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if !r.shouldFollowPartyPeerUnsafe(peer) {
		return
	}
	records := [][]byte{buildPartyResourceBody(12, r.partySelfPeer.uniqueID, pool.hp), buildPartyResourceBody(13, r.partySelfPeer.uniqueID, pool.mp)}
	if _, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, records, "resources", now); err != nil {
		return
	}
	pool.syncPending = false
	for _, body := range records {
		r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", body)
	}
}
