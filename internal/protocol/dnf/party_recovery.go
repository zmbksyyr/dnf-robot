package dnf

import "time"

const (
	partyRealtimeConfirmationsRequired = 2
	partyRealtimeStableDelay           = 2 * time.Second
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

func (r *RobotVo) startPartyRecoveryUnsafe(now time.Time, reason string) bool {
	if !r.partyHumanObserved || r.partyRecovery || r.ReturnSelectPending || now.Before(r.partyRecoveryCooldownUntil) {
		return false
	}
	if !r.sendReturnToCharacterSelectUnsafe(true) {
		r.partyRecoveryCooldownUntil = now.Add(5 * time.Second)
		return false
	}
	recordPartyDebugPacket(r.UID, 0, "--", "CORE", "ORPHAN_DETECTED", "OK", reason, nil)
	return true
}

func (r *RobotVo) maybeConfirmPartyRealtimeUnsafe(now time.Time) {
	if r.partyRealtimeConfirmations != 1 || r.partyRealtimeCandidateAt.IsZero() || now.Sub(r.partyRealtimeCandidateAt) < partyRealtimeStableDelay {
		return
	}
	r.partyRealtimeConfirmations = partyRealtimeConfirmationsRequired
	r.partyRealtimeCandidateAt = time.Time{}
	r.reconcilePartyRealtimeUnsafe(r.partyRealtimeCandidate)
}

func (r *RobotVo) maybeRecoverStalePartyUnsafe(now time.Time) {
	if !r.partyHumanObserved || r.partyRecovery || r.ReturnSelectPending {
		return
	}
	if (!r.partyDungeonLastAt.IsZero() && now.Sub(r.partyDungeonLastAt) < partyStaleRecoveryTimeout) ||
		(!r.partyDungeonEnteredAt.IsZero() && now.Sub(r.partyDungeonEnteredAt) < partyStaleRecoveryTimeout) {
		return
	}
	peers := make([]partyIPPeer, 0, len(r.partyPeers))
	for _, peer := range r.partyPeers {
		if partyPeerIdentityKnown(peer) {
			peers = append(peers, peer)
		}
	}
	if r.partyRosterIsPureRobotUnsafe(r.partySelfPeer, peers) {
		r.startPartyRecoveryUnsafe(now, "known roster contains only robots")
		return
	}
	foundHuman := false
	for _, peer := range peers {
		if peer.accID == 0 {
			return
		}
		if isPartyRobotAccount(peer.accID) {
			continue
		}
		foundHuman = true
		last := r.partyPeerTransportAtUnsafe(peer)
		if last.IsZero() || now.Sub(last) < partyStaleRecoveryTimeout {
			return
		}
	}
	if foundHuman {
		r.startPartyRecoveryUnsafe(now, "human peer transport inactive for 30s")
	}
}

func (r *RobotVo) partyPeerTransportAtUnsafe(peer partyIPPeer) time.Time {
	if !peer.slotKnown || peer.slot >= 4 {
		return time.Time{}
	}
	last := r.partyPeerRouteAt[peer.slot]
	for route := byte(1); route <= 2; route++ {
		if r.partyRouteActivityAt[peer.slot][route].After(last) {
			last = r.partyRouteActivityAt[peer.slot][route]
		}
	}
	if peer.slot == 0 && peer.uniqueID != 0 && r.partyLeaderTransportUnique == peer.uniqueID && r.partyLeaderTransportAt.After(last) {
		last = r.partyLeaderTransportAt
	}
	return last
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
	if r.partyHumanObserved && r.partyRosterIsPureRobotUnsafe(self, peers) && r.startPartyRecoveryUnsafe(time.Now(), "stable realtime roster contains only robots") {
		return
	}
	r.setPartyPeersUnsafe(peers)
}
