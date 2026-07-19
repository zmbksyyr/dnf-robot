package dnf

import (
	"encoding/binary"
	"net"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

func (r *RobotVo) partyPeerForSlotUnsafe(slot byte) partyIPPeer {
	for _, peer := range r.partyPeers {
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
	_, ok := r.townEntityPositions[uniqueID]
	return ok
}

const partyTownPositionMaxAge = 15 * time.Second

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
	var fallback []byte
	for _, candidate := range candidates {
		position, ok := parseTownEntityPosition(candidate.body)
		if !ok {
			continue
		}
		if r.partyEntityKnownUnsafe(position.uniqueID) {
			return candidate.body, true
		}
		if fallback == nil {
			fallback = candidate.body
		}
	}
	return fallback, fallback != nil
}

func (r *RobotVo) selectTownEntityAreaUnsafe(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (townEntityArea, bool) {
	candidates, _ := recvBodyCandidates(cipher, raw, isAnti)
	var fallback townEntityArea
	hasFallback := false
	for _, candidate := range candidates {
		area, ok := parseTownEntityArea(candidate.body)
		if !ok {
			continue
		}
		if r.partyEntityKnownUnsafe(area.uniqueID) {
			return area, true
		}
		if !hasFallback {
			fallback = area
			hasFallback = true
		}
	}
	return fallback, hasFallback
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
	if len(r.townEntityPositions) > 512 {
		cutoff := position.seenAt.Add(-partyTownPositionMaxAge)
		for uniqueID, cached := range r.townEntityPositions {
			if cached.seenAt.Before(cutoff) {
				delete(r.townEntityPositions, uniqueID)
			}
		}
	}
	return position, true
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

const partyPendingTimeout = 15 * time.Second

func (r *RobotVo) partyActiveUnsafe() bool {
	if r.partyPendingPeer != 0 && (r.partyPendingUntil.IsZero() || !time.Now().Before(r.partyPendingUntil)) {
		r.clearPartyPendingUnsafe()
	}
	for _, peer := range r.partyPeers {
		if peer.uniqueID != 0 {
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
}

func (r *RobotVo) clearPartyPendingUnsafe() {
	r.partyPendingPeer = 0
	r.partyPendingUntil = time.Time{}
}

func (r *RobotVo) rememberPartyPeersUnsafe(peers []partyIPPeer) {
	for _, peer := range peers {
		if peer.uniqueID == 0 {
			continue
		}
		known := false
		for i, existing := range r.partyPeers {
			if existing.uniqueID == peer.uniqueID {
				r.partyPeers[i] = mergePartyPeer(r.partyPeers[i], peer)
				known = true
				break
			}
		}
		if known {
			continue
		}
		for i, existing := range r.partyPeers {
			if existing.uniqueID == 0 {
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
			if old.uniqueID == peers[i].uniqueID {
				peers[i] = mergePartyPeer(old, peers[i])
				break
			}
		}
	}
	r.rememberPartyPeersUnsafe(peers)
	for slot := byte(0); slot < 4; slot++ {
		before := partyPeerUniqueIDForSlot(previous, slot)
		after := partyPeerUniqueIDForSlot(r.partyPeers, slot)
		if before != after && (before != 0 || after != 0) {
			r.resetPartyTQOSPeerUnsafe(slot)
		}
	}
	if !r.partyActiveUnsafe() {
		r.partySelfPeer = partyIPPeer{}
		r.resetPartyTQOSTransportUnsafe()
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
			r.partyPeers[i] = partyIPPeer{}
		}
	}
	if !r.partyActiveUnsafe() {
		r.clearPartyUnsafe()
	}
}

func (r *RobotVo) clearPartyUnsafe() {
	r.partySelfPeer = partyIPPeer{}
	r.partyPeers = [4]partyIPPeer{}
	r.clearPartyPendingUnsafe()
	r.townEntityPositions = make(map[uint16]townEntityPosition)
	r.partyDungeonTraceAt = time.Time{}
	r.closePartyRelayUnsafe()
	r.resetPartyTQOSTransportUnsafe()
}

func mergePartyPeer(old, next partyIPPeer) partyIPPeer {
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
