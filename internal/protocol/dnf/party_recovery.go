package dnf

import "time"

const (
	partyRealtimeConfirmationsRequired = 2
	partyStaleRecoveryTimeout          = 30 * time.Second
)

func (r *RobotVo) observePartyAccountsUnsafe(peers []partyIPPeer) {
	for _, peer := range peers {
		if peer.accID != 0 && !isPartyRobotAccount(peer.accID) {
			r.partyHumanObserved = true
			return
		}
	}
}

func (r *RobotVo) partyRosterIsPureRobotUnsafe(self partyIPPeer, peers []partyIPPeer) bool {
	if self.accID != r.UID || !isPartyRobotAccount(self.accID) {
		return false
	}
	for _, peer := range peers {
		if peer.accID == 0 || !isPartyRobotAccount(peer.accID) {
			return false
		}
	}
	return true
}

func (r *RobotVo) startPartyRecoveryUnsafe(now time.Time) bool {
	if !r.partyHumanObserved || r.partyRecovery || r.ReturnSelectPending || now.Before(r.partyRecoveryCooldownUntil) {
		return false
	}
	if !r.sendReturnToCharacterSelectUnsafe(true) {
		r.partyRecoveryCooldownUntil = now.Add(5 * time.Second)
		return false
	}
	return true
}

func (r *RobotVo) maybeRecoverStalePartyUnsafe(now time.Time) {
	if !r.partyHumanObserved || r.partyRecovery || r.ReturnSelectPending || r.partyAnyTransportAt.IsZero() || r.partyRosterAt.IsZero() {
		return
	}
	if now.Sub(r.partyAnyTransportAt) < partyStaleRecoveryTimeout || now.Sub(r.partyRosterAt) < partyStaleRecoveryTimeout {
		return
	}
	if (!r.partyDungeonLastAt.IsZero() && now.Sub(r.partyDungeonLastAt) < partyStaleRecoveryTimeout) ||
		(!r.partyDungeonEnteredAt.IsZero() && now.Sub(r.partyDungeonEnteredAt) < partyStaleRecoveryTimeout) {
		return
	}
	r.startPartyRecoveryUnsafe(now)
}

func partyRealtimeRoster(identities []partyRealtimeIdentity) [4]uint16 {
	var roster [4]uint16
	for _, identity := range identities {
		if identity.slot < 4 {
			roster[identity.slot] = identity.uniqueID
		}
	}
	return roster
}

func (r *RobotVo) reconcilePartyRealtimeUnsafe(roster [4]uint16) {
	self := r.partySelfPeer
	selfSlot := byte(0xff)
	if self.uniqueID != 0 {
		for slot, uniqueID := range roster {
			if uniqueID == self.uniqueID {
				selfSlot = byte(slot)
				break
			}
		}
	}
	if selfSlot == 0xff && self.slotKnown && self.slot < 4 && roster[self.slot] != 0 {
		selfSlot = self.slot
		self.uniqueID = roster[self.slot]
	}
	if selfSlot == 0xff {
		return
	}
	self.slot, self.slotKnown = selfSlot, true
	if self.accID == 0 {
		self.accID = r.UID
	}
	r.setPartySelfPeerUnsafe(self)

	peers := make([]partyIPPeer, 0, 3)
	for slot, uniqueID := range roster {
		if uniqueID == 0 || byte(slot) == selfSlot {
			continue
		}
		peer := partyIPPeer{uniqueID: uniqueID, slot: byte(slot), slotKnown: true}
		for _, old := range r.partyPeers {
			if old.uniqueID == uniqueID || (old.uniqueID == 0 && old.slotKnown && old.slot == byte(slot)) {
				peer = mergePartyPeer(old, peer)
				peer.slot, peer.slotKnown = byte(slot), true
				break
			}
		}
		peers = append(peers, peer)
	}
	r.observePartyAccountsUnsafe(peers)
	if r.partyHumanObserved && r.partyRosterIsPureRobotUnsafe(self, peers) && r.startPartyRecoveryUnsafe(time.Now()) {
		return
	}
	r.setPartyPeersUnsafe(peers)
}
