package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const (
	partyTQOSRetryInterval   = time.Second
	partyTQOSMaxRetries      = 6
	partyTQOSSlowRetry       = 5 * time.Second
	partyTQOSEpochReuseDelay = partyTQOSRetryInterval * partyTQOSMaxRetries
	partyRobotProbeCooldown  = 30 * time.Second
	partyRobotHealthInterval = 10 * time.Second
)

func (r *RobotVo) flushPartyTQOSRepliesUnsafe(conn *net.UDPConn, now time.Time) {
	for slot := byte(0); slot < byte(len(r.partyTQOSReplies)); slot++ {
		peer := r.partyPeerForSlotUnsafe(slot)
		if peer.uniqueID == 0 && peer.accID == 0 {
			continue
		}
		for route := byte(1); route < byte(len(r.partyTQOSReplies[slot])); route++ {
			pending := &r.partyTQOSReplies[slot][route]
			if len(pending.packet) == 0 || pending.acknowledged || pending.nextRetry.IsZero() || now.Before(pending.nextRetry) {
				continue
			}
			if pending.exhausted {
				_, _ = r.sendPartyTransportUnsafe(conn, peer, route, pending.packet)
				pending.nextRetry = now.Add(partyTQOSSlowRetry)
				continue
			}
			if pending.retries >= partyTQOSMaxRetries {
				pending.exhausted = true
				pending.nextRetry = now.Add(partyTQOSSlowRetry)
				sequence := binary.LittleEndian.Uint32(pending.packet[1:5])
				fmt.Printf("[PARTY_TQOS_RETRY_EXHAUSTED] uid=%d peer=%d slot=%d route=%d sequence=%d retries=%d\n", r.UID, peer.accID, slot, route, sequence, pending.retries)
				r.markPartyRouteFailureUnsafe(peer, route, now, "state2 ack timeout")
				continue
			}
			if _, err := r.sendPartyTransportUnsafe(conn, peer, route, pending.packet); err != nil {
				pending.nextRetry = now.Add(partyTQOSRetryInterval)
				continue
			}
			pending.retries++
			pending.nextRetry = now.Add(partyTQOSRetryInterval)
		}
	}
}

func (r *RobotVo) buildPartyUDPAcks(payload []byte, remote *net.UDPAddr) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var senderSlot *byte
	if len(payload) >= 8 && (payload[0] == 0x01 || payload[0] == 0x02) {
		slot := payload[7]
		senderSlot = &slot
	}
	peer, ok := r.partyPeerForUDPUnsafe(remote, senderSlot)
	if !ok {
		r.tracePartyUDPUnsafe("DROP_PEER", remote, senderSlot, 0, 0)
		return nil
	}
	observedEndpoint := partyPeerEndpointMatches(peer.observedIP, peer.observedPort, remote)
	advertisedEndpoint := partyPeerEndpointMatches(peer.outerIP, peer.port, remote)
	if !observedEndpoint && !advertisedEndpoint {
		if senderSlot == nil || !r.partyTQOSPayloadAuthenticatesPeerUnsafe(payload, 1, peer) {
			r.tracePartyUDPUnsafe("DROP_ENDPOINT", remote, senderSlot, peer.accID, len(payload))
			return nil
		}
	}
	if !observedEndpoint {
		peer = r.learnPartyPeerEndpointUnsafe(peer, remote)
	}
	return r.buildPartyTQOSRepliesUnsafe(payload, 1, peer)
}

func (r *RobotVo) partyTQOSPayloadAuthenticatesPeerUnsafe(payload []byte, route byte, peer partyIPPeer) bool {
	if !peer.slotKnown || route < 1 || route > 2 {
		return false
	}
	frames, ok := splitPartyTransportFrames(payload)
	if !ok {
		return false
	}
	var preferred *partyTQOSCodec
	if peer.slot < 4 && r.partyTQOSCodecKnown[peer.slot][route] {
		codec := r.partyTQOSCodecs[peer.slot][route]
		preferred = &codec
	}
	for _, frame := range frames {
		if len(frame) < 9 || frame[0] == 0 || frame[7] != peer.slot {
			continue
		}
		request, ok := parsePartyTQOSPacketWithCodec(frame, route, preferred)
		if ok && request.senderSlot == peer.slot && request.route == route {
			return true
		}
	}
	return false
}

func (r *RobotVo) buildPartyRelayReplies(payload []byte, src uint32) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	peer, ok := r.partyPeerForAccountUnsafe(src)
	if !ok {
		return nil
	}
	return r.buildPartyTQOSRepliesUnsafe(payload, 2, peer)
}

func (r *RobotVo) buildPartyTQOSRepliesUnsafe(payload []byte, route byte, peer partyIPPeer) [][]byte {
	if !r.partySelfPeer.slotKnown || !peer.slotKnown || peer.slot >= 4 || route >= 3 {
		return nil
	}
	frames, ok := splitPartyTransportFrames(payload)
	if !ok {
		return nil
	}
	replies := make([][]byte, 0, len(frames)+1)
	now := time.Now()
	for _, frame := range frames {
		if frame[0] == 0x00 {
			if len(frame) != 8 || frame[1] != peer.slot {
				return nil
			}
			r.rememberPartyPeerRouteUnsafe(peer.slot, route, now)
			generatedACK := r.acknowledgePartyReliableUnsafe(frame, route, peer, now)
			if r.partyTQOSReplyAcknowledgedUnsafe(frame, route, peer) {
				r.markPartyRobotPeerReadyUnsafe(peer, route, "ack", now)
			} else if generatedACK && isPartyRobotAccount(peer.accID) {
				r.markPartyRobotPeerReadyUnsafe(peer, route, "data-ack", now)
			}
			continue
		}
		if frame[7] != peer.slot {
			return nil
		}
		r.rememberPartyPeerRouteUnsafe(peer.slot, route, now)
		var request partyTQOSPacket
		requestOK := false
		var preferred *partyTQOSCodec
		if peer.slot < 4 && route < 3 && r.partyTQOSCodecKnown[peer.slot][route] {
			codec := r.partyTQOSCodecs[peer.slot][route]
			preferred = &codec
		}
		request, requestOK = parsePartyTQOSPacketWithCodec(frame, route, preferred)
		readyFromState2 := requestOK && request.state == 2
		if frame[0] == 0x01 {
			sequence := binary.LittleEndian.Uint32(frame[1:5])
			replies = append(replies, buildPartyTQOSAck(r.partySelfPeer.slot, sequence))
			if !r.partyTQOSReceived[peer.slot][route].accept(sequence) {
				if readyFromState2 {
					r.markPartyRobotPeerReadyUnsafe(peer, route, "state2-duplicate", now)
				}
				continue
			}
		}
		if r.shouldFollowPartyPeerUnsafe(peer) {
			r.rememberPartyDungeonActivityUnsafe(frame, route, peer, now)
			r.queuePartyDungeonFollowUnsafe(frame, peer, now)
		}
		if !requestOK {
			continue
		}
		if request.state == 3 {
			r.beginPartyTQOSEpochUnsafe(peer.slot, route, request, now)
		}
		if request.state == 2 {
			r.markPartyRobotPeerReadyUnsafe(peer, route, "state2", now)
		}
		if peer.slot < 4 && route < 3 {
			r.partyTQOSCodecs[peer.slot][route] = request.codec
			r.partyTQOSCodecKnown[peer.slot][route] = true
		}
		if request.state != 3 && isPartyRobotAccount(peer.accID) && r.partyRobotPeerReady[peer.slot] {
			continue
		}
		nextState, hasNextState := nextPartyTQOSState(request.state)
		if hasNextState {
			var reply []byte
			if nextState == 2 {
				reply, ok = r.partyTQOSReliableReplyUnsafe(peer.slot, route, request)
			} else {
				var sequence uint32
				sequence, ok = r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
				if ok {
					reply = buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, request.flags, nextState, route, request.codec)
				}
			}
			if !ok {
				continue
			}
			replies = append(replies, reply)
		}
	}
	return replies
}

func (r *RobotVo) partyTQOSReplyAcknowledgedUnsafe(frame []byte, route byte, peer partyIPPeer) bool {
	if len(frame) != 8 || frame[0] != 0 || frame[1] != peer.slot || peer.slot >= byte(len(r.partyTQOSReplies)) || route >= byte(len(r.partyTQOSReplies[0])) {
		return false
	}
	pending := &r.partyTQOSReplies[peer.slot][route]
	packet := pending.packet
	if len(packet) < 5 {
		return false
	}
	if binary.LittleEndian.Uint32(frame[2:6]) != binary.LittleEndian.Uint32(packet[1:5])+1 {
		return false
	}
	pending.acknowledged = true
	pending.exhausted = false
	pending.nextRetry = time.Time{}
	return true
}

func (r *RobotVo) markPartyRobotPeerReadyUnsafe(peer partyIPPeer, route byte, reason string, now time.Time) {
	r.setPartyRobotRouteReadyUnsafe(peer, route, true, reason, now)
}

func (r *RobotVo) probePartyRobotPeerHealthUnsafe(conn *net.UDPConn, now time.Time) {
	for slot := byte(0); slot < 4; slot++ {
		peer := r.partyPeerForSlotUnsafe(slot)
		if !isPartyRobotAccount(peer.accID) {
			continue
		}
		for route := byte(1); route <= 2; route++ {
			if !r.partyRobotRouteReady[slot][route] {
				continue
			}
			pending := &r.partyTQOSReplies[slot][route]
			if len(pending.packet) == 0 || !pending.acknowledged {
				continue
			}
			lastActivity := r.partyRouteActivityAt[slot][route]
			if lastActivity.IsZero() || now.Sub(lastActivity) < partyRobotHealthInterval {
				continue
			}
			pending.acknowledged = false
			pending.exhausted = false
			pending.retries = 0
			pending.nextRetry = now.Add(partyTQOSRetryInterval)
			if _, err := r.sendPartyTransportUnsafe(conn, peer, route, pending.packet); err != nil {
				continue
			}
		}
	}
}

func (r *RobotVo) partyTQOSReliableReplyUnsafe(peerSlot, route byte, request partyTQOSPacket) ([]byte, bool) {
	if peerSlot >= byte(len(r.partyTQOSReplies)) || route >= byte(len(r.partyTQOSReplies[0])) {
		return nil, false
	}
	pending := &r.partyTQOSReplies[peerSlot][route]
	if pending.matchesRequestEpoch(request) {
		newerRequest := partyTQOSSequenceAfter(request.sequence, pending.latestRequestSequence)
		if !pending.acknowledged {
			if newerRequest {
				pending.latestRequestSequence = request.sequence
			}
			return pending.packet, true
		}
		if !newerRequest {
			return pending.packet, true
		}
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peerSlot, route, true)
	if !ok {
		return nil, false
	}
	packet := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, request.flags, 2, route, request.codec)
	*pending = partyTQOSReliableReply{
		packet:                packet,
		nextRetry:             time.Now().Add(partyTQOSRetryInterval),
		requestKnown:          true,
		requestType:           request.typ,
		requestFlags:          request.flags,
		requestCodec:          request.codec,
		latestRequestSequence: request.sequence,
	}
	return packet, true
}

func (p *partyTQOSReliableReply) matchesRequestEpoch(request partyTQOSPacket) bool {
	return p != nil && p.requestKnown && len(p.packet) != 0 &&
		p.requestType == request.typ && p.requestFlags == request.flags && p.requestCodec == request.codec
}

func partyTQOSSequenceAfter(sequence, previous uint32) bool {
	return int32(sequence-previous) > 0
}

type partyTQOSEpoch struct {
	flags       byte
	codec       partyTQOSCodec
	startedAt   time.Time
	initialized bool
}

func (e partyTQOSEpoch) matches(request partyTQOSPacket) bool {
	return e.initialized && e.flags == request.flags && e.codec == request.codec
}

func (r *RobotVo) beginPartyTQOSEpochUnsafe(peerSlot, route byte, request partyTQOSPacket, now time.Time) {
	if peerSlot >= byte(len(r.partyTQOSReplies)) || route >= byte(len(r.partyTQOSReplies[0])) {
		return
	}
	epoch := &r.partyTQOSEpochs[peerSlot][route]
	if epoch.matches(request) && now.Sub(epoch.startedAt) < partyTQOSEpochReuseDelay {
		return
	}
	r.resetPartyTQOSRouteUnsafe(peerSlot, route)
	epoch = &r.partyTQOSEpochs[peerSlot][route]
	*epoch = partyTQOSEpoch{
		flags:       request.flags,
		codec:       request.codec,
		startedAt:   now,
		initialized: true,
	}
}

func (r *RobotVo) tracePartyUDPUnsafe(reason string, remote *net.UDPAddr, senderSlot *byte, peer uint32, size int) {
	if !isPartyRobotAccount(peer) && reason != "DROP_PEER" {
		return
	}
	if reason == "DROP_PEER" {
		if !isPartyRobotAccount(r.partySelfPeer.accID) || remote == nil || remote.IP == nil {
			return
		}
		localIP := r.partySelfPeer.outerIP
		if localIP == nil {
			localIP = r.partySelfPeer.innerIP
		}
		if localIP == nil || !localIP.Equal(remote.IP) {
			return
		}
	}
	now := time.Now()
	if now.Before(r.partyUDPDiagAt) {
		return
	}
	r.partyUDPDiagAt = now.Add(30 * time.Second)
	slot := -1
	if senderSlot != nil {
		slot = int(*senderSlot)
	}
	remoteText := "<nil>"
	if remote != nil {
		remoteText = remote.String()
	}
	fmt.Printf("[PARTY_ROBOT_UDP_%s] uid=%d peer=%d sender_slot=%d size=%d remote=%s\n", reason, r.UID, peer, slot, size, remoteText)
}

func (r *RobotVo) nextPartyTQOSSequenceUnsafe(peerSlot, route byte, reliable bool) (uint32, bool) {
	if peerSlot >= byte(len(r.partyTQOSSeq)) || route >= byte(len(r.partyTQOSSeq[0])) {
		return 0, false
	}
	if reliable {
		sequence := r.partyTQOSReliableSeq[peerSlot][route]
		r.partyTQOSReliableSeq[peerSlot][route]++
		return sequence, true
	}
	sequence := r.partyTQOSSeq[peerSlot][route]
	r.partyTQOSSeq[peerSlot][route]++
	return sequence, true
}

func (r *RobotVo) rememberPartyPeerRouteUnsafe(peerSlot, route byte, now time.Time) {
	if peerSlot >= byte(len(r.partyPeerRoute)) || (route != 1 && route != 2) {
		return
	}
	peer := r.partyPeerForSlotUnsafe(peerSlot)
	r.markPartyRouteHealthyUnsafe(peer, route, now)
}

func (r *RobotVo) partyRouteForPeerUnsafe(peerSlot byte) byte {
	peer := r.partyPeerForSlotUnsafe(peerSlot)
	now := time.Now()
	if peerSlot < byte(len(r.partyPeerRoute)) && !r.partyPeerRouteAt[peerSlot].IsZero() {
		route := r.partyPeerRoute[peerSlot]
		if (!isPartyRobotAccount(peer.accID) || r.partyRobotRouteReady[peerSlot][route]) && r.partyRouteEligibleUnsafe(peer, route, now) {
			return route
		}
	}
	if isPartyRobotAccount(peer.accID) && peerSlot < 4 {
		for route := byte(1); route <= 2; route++ {
			if r.partyRobotRouteReady[peerSlot][route] && r.partyRouteEligibleUnsafe(peer, route, now) {
				return route
			}
		}
	}
	if r.partyRouteEligibleUnsafe(peer, 1, now) {
		return 1
	}
	if r.partyRouteEligibleUnsafe(peer, 2, now) {
		return 2
	}
	if r.partyRouteAvailableUnsafe(peer, 1) {
		return 1
	}
	if r.partyRouteAvailableUnsafe(peer, 2) {
		return 2
	}
	return 1
}

func (r *RobotVo) partyRouteAvailableUnsafe(peer partyIPPeer, route byte) bool {
	switch route {
	case 1:
		_, ok := partyPeerUDPAddr(peer)
		return ok && r.partyUDPConn != nil && r.partyUDPRunning
	case 2:
		return peer.accID != 0 && r.partyRelayConn != nil
	default:
		return false
	}
}

func (r *RobotVo) startPartyRobotPeerNegotiationUnsafe() {
	if !r.partySelfPeer.slotKnown || !isPartyRobotAccount(r.partySelfPeer.accID) || (r.partyUDPConn == nil && r.partyRelayConn == nil) {
		return
	}
	for _, peer := range r.partyPeers {
		if !peer.slotKnown || peer.slot >= 4 || r.partyRobotPeerReady[peer.slot] || !isPartyRobotAccount(peer.accID) {
			continue
		}
		if r.partySelfPeer.accID == peer.accID {
			continue
		}
		now := time.Now()
		if r.partyRobotProbeCount[peer.slot] >= 4 {
			if now.Sub(r.partyRobotProbeAt[peer.slot]) < partyRobotProbeCooldown {
				continue
			}
			r.partyRobotProbeCount[peer.slot] = 0
		}
		if !r.partyRobotProbeAt[peer.slot].IsZero() && now.Sub(r.partyRobotProbeAt[peer.slot]) < 750*time.Millisecond {
			continue
		}
		attempt := int(r.partyRobotProbeCount[peer.slot]) + 1
		route := r.partyRouteForPeerUnsafe(peer.slot)
		if !r.partyRouteAvailableUnsafe(peer, route) {
			continue
		}
		if r.sendPartyRobotPeerProbeRouteUnsafe(peer, route, attempt) {
			r.partyRobotProbeAt[peer.slot] = now
			r.partyRobotProbeCount[peer.slot]++
		}
	}
}

func (r *RobotVo) sendPartyRobotPeerProbeRouteUnsafe(peer partyIPPeer, route byte, attempt int) bool {
	if !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 {
		return false
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
	if !ok {
		return false
	}
	payload := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, 0, 3, route, partyTQOSCodec{key: 0x7e})
	destination, err := r.sendPartyTransportUnsafe(r.partyUDPConn, peer, route, payload)
	if err != nil {
		fmt.Printf("[PARTY_ROBOT_PROBE_ERROR] uid=%d peer=%d route=%d attempt=%d destination=%s err=%v\n", r.UID, peer.accID, route, attempt, destination, err)
		return false
	}
	if attempt == 1 {
		fmt.Printf("[PARTY_ROBOT_PROBE_CYCLE] uid=%d peer=%d slot=%d route=%d sequence=%d destination=%s\n", r.UID, peer.accID, peer.slot, route, sequence, destination)
	}
	return true
}

func (r *RobotVo) resetPartyTQOSTransportUnsafe() {
	r.partyTQOSSeq = [4][3]uint32{}
	r.partyTQOSReliableSeq = [4][3]uint32{}
	r.partyTQOSReplies = [4][3]partyTQOSReliableReply{}
	r.partyReliablePending = [4][3][]partyReliablePending{}
	r.partyTQOSReceived = [4][3]partyTQOSReceiveWindow{}
	r.partyTQOSEpochs = [4][3]partyTQOSEpoch{}
	r.partyTQOSCodecs = [4][3]partyTQOSCodec{}
	r.partyTQOSCodecKnown = [4][3]bool{}
	r.partyRobotProbeAt = [4]time.Time{}
	r.partyRobotProbeCount = [4]uint8{}
	r.partyRobotPeerReady = [4]bool{}
	r.partyRobotRouteReady = [4][3]bool{}
	r.partyPeerRoute = [4]byte{}
	r.partyPeerRouteAt = [4]time.Time{}
	r.partyRouteActivityAt = [4][3]time.Time{}
	r.partyRouteBlockedUntil = [4][3]time.Time{}
	r.partyRouteFailures = [4][3]uint8{}
	r.partyRouteRecoveryAt = [4][3]time.Time{}
	r.partyRouteDiagAt = [4][3]time.Time{}
	r.partyRuntimeDiagAt = time.Time{}
	r.partyDungeonFollow = nil
	r.partyDungeonEnteredAt = time.Time{}
	r.partyDungeonLastAt = time.Time{}
	r.partyDungeonFlags = 0
	r.partySkillNextAt = time.Time{}
	r.partySkillRecoverAt = time.Time{}
	r.partySkillBlockedUntil = time.Time{}
}

func (r *RobotVo) resetPartyTQOSPeerUnsafe(slot byte) {
	if slot >= byte(len(r.partyTQOSSeq)) {
		return
	}
	r.partyTQOSSeq[slot] = [3]uint32{}
	r.partyTQOSReliableSeq[slot] = [3]uint32{}
	r.partyTQOSReplies[slot] = [3]partyTQOSReliableReply{}
	r.partyReliablePending[slot] = [3][]partyReliablePending{}
	r.partyTQOSReceived[slot] = [3]partyTQOSReceiveWindow{}
	r.partyTQOSEpochs[slot] = [3]partyTQOSEpoch{}
	r.partyTQOSCodecs[slot] = [3]partyTQOSCodec{}
	r.partyTQOSCodecKnown[slot] = [3]bool{}
	r.partyRobotProbeAt[slot] = time.Time{}
	r.partyRobotProbeCount[slot] = 0
	r.partyRobotPeerReady[slot] = false
	r.partyRobotRouteReady[slot] = [3]bool{}
	r.partyPeerRoute[slot] = 0
	r.partyPeerRouteAt[slot] = time.Time{}
	r.partyRouteActivityAt[slot] = [3]time.Time{}
	r.partyRouteBlockedUntil[slot] = [3]time.Time{}
	r.partyRouteFailures[slot] = [3]uint8{}
	r.partyRouteRecoveryAt[slot] = [3]time.Time{}
	r.partyRouteDiagAt[slot] = [3]time.Time{}
	if len(r.partyDungeonFollow) > 0 {
		kept := r.partyDungeonFollow[:0]
		for _, pending := range r.partyDungeonFollow {
			if pending.peerSlot != slot {
				kept = append(kept, pending)
			}
		}
		r.partyDungeonFollow = kept
	}
}
