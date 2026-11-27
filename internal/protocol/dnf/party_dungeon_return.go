package dnf

import foundationlog "robot/internal/foundation/log"

// Dungeon membership outlives both combat (the reward screen) and the party
// transport. Only a server town notice or character logout ends it.
func (r *RobotVo) sendPartyReturnTownUnsafe(reason string) bool {
	if r.State != StateRun || r.ReturnSelectPending || !r.partyDungeonInMap || r.partyDungeonReturnPending {
		return false
	}
	pkt, err := buildSendPacket(45, uint16(r.PacketID), nil, r.Cipher)
	if err != nil || !r.sendRaw(pkt) {
		return false
	}
	r.PacketID++
	r.partyDungeonReturnPending = true
	r.clearPartyDungeonRuntimeUnsafe()
	foundationlog.Robotf("[PARTY_RETURN_TOWN_SENT] uid=%d reason=%s\n", r.UID, reason)
	return true
}

func (r *RobotVo) confirmPartyReturnTownUnsafe() {
	if r.partyDungeonInMap || r.partyDungeonReturnPending {
		foundationlog.Robotf("[PARTY_RETURN_TOWN_CONFIRMED] uid=%d\n", r.UID)
	}
	r.partyDungeonInMap = false
	r.partyDungeonReturnPending = false
	r.clearPartyDungeonRuntimeUnsafe()
}

func (r *RobotVo) observePartyTownReturnUnsafe(uniqueID uint16) {
	if uniqueID != 0 && (uniqueID == r.characterUniqueID || uniqueID == r.partySelfPeer.uniqueID) {
		r.confirmPartyReturnTownUnsafe()
		return
	}
	if leader, ok := r.partyLeaderUniqueIDUnsafe(); ok && uniqueID == leader {
		r.sendPartyReturnTownUnsafe("leader returned to town")
	}
}
