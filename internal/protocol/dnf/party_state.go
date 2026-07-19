package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

func (r *RobotVo) partyPeerForSlotUnsafe(slot byte) partyIPPeer {
	return partyPeerForSlot(r.partyPeers, slot)
}

func partyPeerForSlot(peers [4]partyIPPeer, slot byte) partyIPPeer {
	for _, peer := range peers {
		if peer.slotKnown && peer.slot == slot {
			return peer
		}
	}
	return partyIPPeer{}
}

func (r *RobotVo) partyPeerForAccountUnsafe(accID uint32) (partyIPPeer, bool) {
	if accID == 0 {
		return partyIPPeer{}, false
	}
	for _, peer := range r.partyPeers {
		if peer.accID == accID {
			return peer, true
		}
	}
	return partyIPPeer{}, false
}

func (r *RobotVo) partyEntityKnownUnsafe(uniqueID uint16) bool {
	if r.partyConfirmedPeerUnsafe(uniqueID) {
		return true
	}
	_, ok := r.townEntityPositions[uniqueID]
	return ok
}

func (r *RobotVo) partyConfirmedPeerUnsafe(uniqueID uint16) bool {
	if uniqueID == 0 {
		return false
	}
	if r.partySelfPeer.uniqueID == uniqueID {
		return true
	}
	for _, peer := range r.partyPeers {
		if peer.uniqueID == uniqueID {
			return true
		}
	}
	return false
}

func (r *RobotVo) rememberPartyRecvSourceUnsafe(source recvBodySource) {
	if source == recvBodySourceDecrypted || source == recvBodySourcePlain {
		r.partyRecvSource = source
	}
}

const (
	partyTownPositionMaxAge      = 15 * time.Second
	partyTownEntitySweepInterval = 2 * time.Second
	partyTownEntitySoftLimit     = 1024
	partyTownEntityHardLimit     = 2048
)

type townEntityPosition struct {
	uniqueID uint16
	x        uint16
	y        uint16
	moveType byte
	speed    uint16
	seenAt   time.Time
}

type townEntityArea struct {
	uniqueID uint16
	village  uint8
	area     uint8
	x        uint16
	y        uint16
}

func parseTownEntityPosition(data []byte) (townEntityPosition, bool) {
	if len(data) < 9 {
		return townEntityPosition{}, false
	}
	uniqueID := binary.LittleEndian.Uint16(data[:2])
	if uniqueID == 0 {
		return townEntityPosition{}, false
	}
	return townEntityPosition{
		uniqueID: uniqueID,
		x:        binary.LittleEndian.Uint16(data[2:4]),
		y:        binary.LittleEndian.Uint16(data[4:6]),
		moveType: data[6],
		speed:    binary.LittleEndian.Uint16(data[7:9]),
		seenAt:   time.Now(),
	}, true
}

func parseTownEntityArea(data []byte) (townEntityArea, bool) {
	if len(data) < 8 {
		return townEntityArea{}, false
	}
	uniqueID := binary.LittleEndian.Uint16(data[:2])
	if uniqueID == 0 {
		return townEntityArea{}, false
	}
	return townEntityArea{
		uniqueID: uniqueID,
		village:  data[2],
		area:     data[3],
		x:        binary.LittleEndian.Uint16(data[4:6]),
		y:        binary.LittleEndian.Uint16(data[6:8]),
	}, true
}

func (r *RobotVo) selectTownEntityPositionBodyUnsafe(cipher *crypt.DNFCipher, raw []byte, isAnti bool) ([]byte, bool) {
	candidates, _ := recvBodyCandidates(cipher, raw, isAnti)
	valid := make([]recvBodyCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		position, ok := parseTownEntityPosition(candidate.body)
		if !ok {
			continue
		}
		valid = append(valid, candidate)
		if r.partyEntityKnownUnsafe(position.uniqueID) {
			r.rememberPartyRecvSourceUnsafe(candidate.source)
			return candidate.body, true
		}
	}
	if len(valid) == 1 {
		r.rememberPartyRecvSourceUnsafe(valid[0].source)
		return valid[0].body, true
	}
	for _, candidate := range valid {
		if candidate.source == r.partyRecvSource && r.partyRecvSource != recvBodySourceUnknown {
			return candidate.body, true
		}
	}
	for _, candidate := range valid {
		if candidate.source == recvBodySourcePlain {
			return candidate.body, true
		}
	}
	if len(valid) > 0 {
		return valid[0].body, true
	}
	return nil, false
}

func (r *RobotVo) selectTownEntityAreaUnsafe(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (townEntityArea, bool) {
	candidates, _ := recvBodyCandidates(cipher, raw, isAnti)
	type areaCandidate struct {
		area   townEntityArea
		source recvBodySource
	}
	valid := make([]areaCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		area, ok := parseTownEntityArea(candidate.body)
		if !ok {
			continue
		}
		valid = append(valid, areaCandidate{area: area, source: candidate.source})
		if r.partyEntityKnownUnsafe(area.uniqueID) {
			r.rememberPartyRecvSourceUnsafe(candidate.source)
			return area, true
		}
	}
	if len(valid) == 1 {
		r.rememberPartyRecvSourceUnsafe(valid[0].source)
		return valid[0].area, true
	}
	for _, candidate := range valid {
		if candidate.source == r.partyRecvSource && r.partyRecvSource != recvBodySourceUnknown {
			return candidate.area, true
		}
	}
	for _, candidate := range valid {
		if candidate.source == recvBodySourcePlain {
			return candidate.area, true
		}
	}
	if len(valid) > 0 {
		return valid[0].area, true
	}
	return townEntityArea{}, false
}

func (r *RobotVo) rememberTownEntityUnsafe(data []byte) (townEntityPosition, bool) {
	position, ok := parseTownEntityPosition(data)
	if !ok {
		return townEntityPosition{}, false
	}
	if r.townEntityPositions == nil {
		r.townEntityPositions = make(map[uint16]townEntityPosition)
	}
	r.townEntityPositions[position.uniqueID] = position
	r.pruneTownEntitiesUnsafe(position.seenAt)
	return position, true
}

func (r *RobotVo) pruneTownEntitiesUnsafe(now time.Time) {
	if len(r.townEntityPositions) <= partyTownEntitySoftLimit {
		return
	}
	if len(r.townEntityPositions) <= partyTownEntityHardLimit && now.Before(r.townEntitySweepAt) {
		return
	}
	r.townEntitySweepAt = now.Add(partyTownEntitySweepInterval)
	cutoff := now.Add(-partyTownPositionMaxAge)
	for uniqueID, cached := range r.townEntityPositions {
		if cached.seenAt.Before(cutoff) {
			delete(r.townEntityPositions, uniqueID)
		}
	}
	if len(r.townEntityPositions) <= partyTownEntityHardLimit {
		return
	}
	entries := make([]townEntityPosition, 0, len(r.townEntityPositions))
	for _, cached := range r.townEntityPositions {
		entries = append(entries, cached)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].seenAt.Before(entries[j].seenAt) })
	for _, cached := range entries[:len(entries)-partyTownEntitySoftLimit] {
		delete(r.townEntityPositions, cached.uniqueID)
	}
}

func (r *RobotVo) partyLeaderUniqueIDUnsafe() (uint16, bool) {
	if !r.partySelfPeer.slotKnown || r.partySelfPeer.slot == 0 {
		return 0, false
	}
	for _, peer := range r.partyPeers {
		if peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0 {
			return peer.uniqueID, true
		}
	}
	return 0, false
}

func (r *RobotVo) followCachedPartyLeaderTownPositionUnsafe() bool {
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok {
		return false
	}
	position, ok := r.townEntityPositions[leaderID]
	if !ok || position.seenAt.IsZero() || time.Since(position.seenAt) > partyTownPositionMaxAge {
		return false
	}
	return r.followPartyLeaderTownPositionUnsafe(position)
}

func (r *RobotVo) followPartyLeaderTownAreaUnsafe(area townEntityArea) bool {
	if r.State != StateRun {
		return false
	}
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok || area.uniqueID != leaderID || (r.CurVillage == area.village && r.CurArea == area.area) {
		return false
	}
	r.setAreaFromLocked(area.village, area.area, area.x, area.y, uint16(r.CurVillage), uint16(r.CurArea))
	return r.CurVillage == area.village && r.CurArea == area.area
}

func (r *RobotVo) followPartyLeaderTownPositionUnsafe(position townEntityPosition) bool {
	if r.State != StateRun || position.uniqueID == 0 {
		return false
	}
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok || position.uniqueID != leaderID || (r.CurX == position.x && r.CurY == position.y) {
		return false
	}
	moveType := position.moveType
	if moveType == 0 {
		moveType = 5
	}
	speed := position.speed
	if speed == 0 {
		speed = 100
	}
	return r.setPositionUnsafe(position.x, position.y, moveType, speed)
}

const (
	partyPendingTimeout      = 15 * time.Second
	partyInviteFallbackDelay = 3 * time.Second
)

type partyInviteFallbackState struct {
	data        [8]byte
	source      recvBodySource
	primaryPeer uint16
	primaryType byte
	due         time.Time
}

func (r *RobotVo) partyActiveUnsafe() bool {
	if r.partyPendingPeer != 0 && (r.partyPendingUntil.IsZero() || !time.Now().Before(r.partyPendingUntil)) {
		fmt.Printf("[PARTY_SNAPSHOT_TIMEOUT] uid=%d peer_unique_id=%d wait=%s\n", r.UID, r.partyPendingPeer, partyPendingTimeout)
		r.clearPartyPendingUnsafe()
	}
	for _, peer := range r.partyPeers {
		if partyPeerIdentityKnown(peer) {
			return true
		}
	}
	return r.partyPendingPeer != 0
}

func (r *RobotVo) setPartyPendingUnsafe(uniqueID uint16) {
	if uniqueID == 0 {
		r.clearPartyPendingUnsafe()
		return
	}
	r.partyPendingPeer = uniqueID
	r.partyPendingUntil = time.Now().Add(partyPendingTimeout)
	r.ensurePartyUDPLoopUnsafe()
	r.ensurePartySupervisorUnsafe()
}

func (r *RobotVo) clearPartyPendingUnsafe() {
	r.partyPendingPeer = 0
	r.partyPendingUntil = time.Time{}
	r.clearPartyInviteFallbackUnsafe()
}

func (r *RobotVo) schedulePartyInviteFallbackUnsafe(selected peerResponseCandidate, candidate *peerResponseCandidate) {
	r.clearPartyInviteFallbackUnsafe()
	if len(selected.data) < 2 || candidate == nil || candidate.typ != peerRequestParty || len(candidate.data) < 8 {
		return
	}
	if selected.typ != peerRequestParty && (selected.typ != peerRequestTrade || !selected.canonical || !candidate.canonical) {
		return
	}
	primaryPeer := binary.LittleEndian.Uint16(selected.data[:2])
	copy(r.partyInviteFallback.data[:], candidate.data[:8])
	r.partyInviteFallback.source = candidate.source
	r.partyInviteFallback.primaryPeer = primaryPeer
	r.partyInviteFallback.primaryType = selected.typ
	r.partyInviteFallback.due = time.Now().Add(partyInviteFallbackDelay)
	epoch := r.partyInviteEpoch
	r.partyInviteTimer = time.AfterFunc(partyInviteFallbackDelay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.flushPartyInviteFallbackUnsafe(time.Now(), epoch)
	})
}

func (r *RobotVo) clearPartyInviteFallbackUnsafe() {
	r.partyInviteEpoch++
	if r.partyInviteTimer != nil {
		r.partyInviteTimer.Stop()
		r.partyInviteTimer = nil
	}
	r.partyInviteFallback = partyInviteFallbackState{}
}

func (r *RobotVo) flushPartyInviteFallbackUnsafe(now time.Time, epoch uint64) bool {
	if epoch != r.partyInviteEpoch || r.partyInviteFallback.due.IsZero() || now.Before(r.partyInviteFallback.due) {
		return false
	}
	fallback := r.partyInviteFallback
	r.clearPartyInviteFallbackUnsafe()
	if r.State != StateRun || (fallback.primaryType == peerRequestParty && r.partyPendingPeer != fallback.primaryPeer) {
		return false
	}
	if fallback.primaryType == peerRequestTrade && (!r.LastTradeState || r.LastTradeID != fallback.primaryPeer) {
		return false
	}
	for _, peer := range r.partyPeers {
		if peer.uniqueID != 0 {
			return false
		}
	}
	pkt, err := buildSendPacket(11, uint16(r.PacketID), fallback.data[:], r.Cipher)
	r.PacketID++
	if err != nil {
		fmt.Printf("[PARTY_FALLBACK_BUILD_ERROR] uid=%d err=%v\n", r.UID, err)
		return false
	}
	if !r.sendRaw(pkt) {
		fmt.Printf("[PARTY_FALLBACK_SEND_ERROR] uid=%d\n", r.UID)
		return false
	}
	if fallback.primaryType == peerRequestTrade && r.LastTradeState && r.LastTradeID == fallback.primaryPeer {
		r.invalidateTradeQuoteUnsafe()
		r.clearTradeUnsafe()
	}
	uniqueID := binary.LittleEndian.Uint16(fallback.data[:2])
	r.rememberPartyRecvSourceUnsafe(fallback.source)
	r.setPartyPendingUnsafe(uniqueID)
	r.ensurePartyRelayUnsafe()
	fmt.Printf("[PARTY_FALLBACK_ACCEPT] uid=%d peer_unique_id=%d request_id=%d source=%s\n",
		r.UID, uniqueID, binary.LittleEndian.Uint32(fallback.data[3:7]), fallback.source)
	return true
}

func (r *RobotVo) clearConfirmedTradeFallbackUnsafe() {
	if r.partyInviteFallback.primaryType == peerRequestTrade {
		r.clearPartyInviteFallbackUnsafe()
	}
}

func (r *RobotVo) rememberPartyPeersUnsafe(peers []partyIPPeer) {
	for _, peer := range peers {
		if !partyPeerIdentityKnown(peer) {
			continue
		}
		known := false
		for i, existing := range r.partyPeers {
			if partyPeerSameIdentity(existing, peer) {
				r.partyPeers[i] = mergePartyPeer(r.partyPeers[i], peer)
				known = true
				break
			}
		}
		if known {
			continue
		}
		for i, existing := range r.partyPeers {
			if !partyPeerIdentityKnown(existing) {
				r.partyPeers[i] = peer
				known = true
				break
			}
		}
		if !known {
			copy(r.partyPeers[1:], r.partyPeers[:len(r.partyPeers)-1])
			r.partyPeers[0] = peer
		}
	}
}

func (r *RobotVo) setPartyPeersUnsafe(peers []partyIPPeer) {
	r.clearPartyPendingUnsafe()
	previous := r.partyPeers
	r.partyPeers = [4]partyIPPeer{}
	for i := range peers {
		for _, old := range previous {
			if partyPeerSameIdentity(old, peers[i]) {
				peers[i] = mergePartyPeer(old, peers[i])
				if !partyPeerAdvertisedEndpointEqual(old, peers[i]) {
					peers[i].observedIP = nil
					peers[i].observedPort = 0
				}
				break
			}
		}
	}
	r.rememberPartyPeersUnsafe(peers)
	r.ensurePartyUDPLoopUnsafe()
	r.ensurePartySupervisorUnsafe()
	for slot := byte(0); slot < 4; slot++ {
		before := partyPeerForSlot(previous, slot)
		after := partyPeerForSlot(r.partyPeers, slot)
		uniqueChanged := before.uniqueID != 0 && after.uniqueID != 0 && before.uniqueID != after.uniqueID
		identityChanged := uniqueChanged || (!partyPeerSameIdentity(before, after) && (partyPeerIdentityKnown(before) || partyPeerIdentityKnown(after)))
		endpointChanged := partyPeerSameIdentity(before, after) && partyPeerIdentityKnown(before) && !partyPeerAdvertisedEndpointEqual(before, after)
		if identityChanged || endpointChanged {
			reason := "identity changed"
			if endpointChanged {
				reason = "endpoint changed"
			}
			r.logPartyPeerTransportResetUnsafe(before, reason)
			r.resetPartyTQOSPeerUnsafe(slot)
		}
	}
	if !r.partyActiveUnsafe() {
		r.logPartyTransportClearedUnsafe("empty snapshot")
		r.stopPartySupervisorUnsafe()
		r.partySelfPeer = partyIPPeer{}
		r.partySelfRefreshAttempts = 0
		r.resetPartyTQOSTransportUnsafe()
	}
}

func (r *RobotVo) setPartySelfPeerUnsafe(peer partyIPPeer) {
	previous := r.partySelfPeer
	uniqueChanged := previous.uniqueID != 0 && peer.uniqueID != 0 && previous.uniqueID != peer.uniqueID
	if partyPeerSameIdentity(previous, peer) {
		peer = mergePartyPeer(previous, peer)
	} else if !partyPeerIdentityKnown(peer) && previous.accID == r.UID {
		peer = mergePartyPeer(previous, peer)
	}
	r.partySelfPeer = peer
	if uniqueChanged {
		r.logPartyTransportClearedUnsafe("self identity changed")
		r.resetPartyTQOSTransportUnsafe()
	}
	if peer.uniqueID != 0 {
		r.partySelfRefreshAt = time.Time{}
		r.partySelfRefreshBackoff = 0
		r.partySelfRefreshAttempts = 0
	} else if peer.accID == r.UID && peer.slotKnown && r.partySelfRefreshAt.IsZero() {
		r.partySelfRefreshAt = time.Now().Add(3 * time.Second)
	}
}

func (r *RobotVo) rememberPartyRealtimeIdentitiesUnsafe(identities []partyRealtimeIdentity) {
	next := [4]uint16{}
	for _, identity := range identities {
		if identity.slot < 4 {
			next[identity.slot] = identity.uniqueID
		}
	}
	r.partyRealtimeUnique = next
	r.applyPartyRealtimeIdentitiesUnsafe()
}

func (r *RobotVo) applyPartyRealtimeIdentitiesUnsafe() {
	self := r.partySelfPeer
	selfRecovered := false
	if self.slotKnown && self.slot < 4 {
		if uniqueID := r.partyRealtimeUnique[self.slot]; uniqueID != 0 && uniqueID != self.uniqueID {
			selfRecovered = self.uniqueID == 0
			self.uniqueID = uniqueID
			r.setPartySelfPeerUnsafe(self)
		}
	}

	peers := make([]partyIPPeer, 0, len(r.partyPeers))
	peersChanged := false
	for _, peer := range r.partyPeers {
		if !partyPeerIdentityKnown(peer) {
			continue
		}
		if peer.slotKnown && peer.slot < 4 {
			if uniqueID := r.partyRealtimeUnique[peer.slot]; uniqueID != 0 && uniqueID != peer.uniqueID {
				peer.uniqueID = uniqueID
				peersChanged = true
			}
		}
		peers = append(peers, peer)
	}
	if peersChanged {
		r.setPartyPeersUnsafe(peers)
	}
	if selfRecovered {
		fmt.Printf("[PARTY_SELF_ID_RECOVERED] uid=%d slot=%d unique=%d source=realtime\n", r.UID, r.partySelfPeer.slot, r.partySelfPeer.uniqueID)
	}
}

func (r *RobotVo) removePartyPeerUnsafe(uniqueID uint16) {
	if uniqueID != 0 {
		r.clearPartyPendingUnsafe()
	}
	if uniqueID == 0 || uniqueID == r.partySelfPeer.uniqueID {
		r.clearPartyUnsafe()
		return
	}
	for i, peer := range r.partyPeers {
		if peer.uniqueID == uniqueID {
			r.logPartyPeerTransportResetUnsafe(peer, "peer removed")
			r.partyPeers[i] = partyIPPeer{}
			if peer.slotKnown {
				r.resetPartyTQOSPeerUnsafe(peer.slot)
			}
		}
	}
	if !r.partyActiveUnsafe() {
		r.clearPartyUnsafe()
	}
}

func (r *RobotVo) clearPartyUnsafe() {
	r.logPartyTransportClearedUnsafe("party cleared")
	r.stopPartySupervisorUnsafe()
	r.partySelfPeer = partyIPPeer{}
	r.partySelfRefreshAt = time.Time{}
	r.partySelfRefreshBackoff = 0
	r.partySelfRefreshAttempts = 0
	r.partyRealtimeUnique = [4]uint16{}
	r.partyPeers = [4]partyIPPeer{}
	r.clearPartyPendingUnsafe()
	r.townEntityPositions = make(map[uint16]townEntityPosition)
	r.townEntitySweepAt = time.Time{}
	r.closePartyRelayUnsafe()
	r.resetPartyTQOSTransportUnsafe()
}

func mergePartyPeer(old, next partyIPPeer) partyIPPeer {
	if next.uniqueID == 0 {
		next.uniqueID = old.uniqueID
	}
	if next.accID == 0 {
		next.accID = old.accID
	}
	if !next.slotKnown && old.slotKnown {
		next.slot = old.slot
		next.slotKnown = true
	}
	if next.innerIP == nil {
		next.innerIP = old.innerIP
	}
	if next.outerIP == nil {
		next.outerIP = old.outerIP
	}
	if next.port == 0 {
		next.port = old.port
	}
	if next.observedIP == nil {
		next.observedIP = old.observedIP
	}
	if next.observedPort == 0 {
		next.observedPort = old.observedPort
	}
	if next.natType == 0 {
		next.natType = old.natType
	}
	if next.mtu == 0 {
		next.mtu = old.mtu
	}
	return next
}

func partyPeerIdentityKnown(peer partyIPPeer) bool {
	return peer.uniqueID != 0 || peer.accID != 0
}

func partyPeerSameIdentity(left, right partyIPPeer) bool {
	if left.accID != 0 && right.accID != 0 {
		return left.accID == right.accID
	}
	return left.uniqueID != 0 && left.uniqueID == right.uniqueID
}

func partyPeerAdvertisedEndpointEqual(left, right partyIPPeer) bool {
	return partyIPEqual(left.outerIP, right.outerIP) && left.port == right.port
}

func partyIPEqual(left, right net.IP) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0
	}
	return left.Equal(right)
}

type partyIPPeer struct {
	uniqueID     uint16
	accID        uint32
	slot         byte
	slotKnown    bool
	innerIP      net.IP
	outerIP      net.IP
	port         uint16
	observedIP   net.IP
	observedPort uint16
	natType      byte
	mtu          uint32
}
