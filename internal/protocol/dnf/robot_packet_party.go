package dnf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

func (r *RobotVo) handlePartyPacketUnsafe(packet robotInboundPacket) {
	// Command 7 keeps StateRun until the server replies. Late character
	// notifications must not restart loading, follow or peer negotiation.
	// Its response is consumed separately by handleReturnToSelectPacketUnsafe.
	if r.ReturnSelectPending {
		return
	}
	if r.partyDungeonReturnPending && packet.typ >= 28 && packet.typ <= 31 {
		return
	}
	switch packet.typ {
	case 27:
		if r.State == StateRun && packet.flag == 0 {
			if _, _, _, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti); err == nil {
				r.partyDungeonLoading = true
				r.reportPartyLoadingUnsafe(time.Now())
			}
		}
	case 28, 29:
		if r.State != StateRun || packet.flag != 0 || packet.size < 15 {
			return
		}
		var mapID uint32
		if packet.typ == 29 {
			body, ok := r.selectRoomLoadingBodyUnsafe(packet)
			if !ok {
				if partyDebugEnabled() {
					recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "DUNGEON_NOTIFY", "FAIL",
						"type=29 invalid-room-info", packet.data)
				}
				return
			}
			r.partyDungeonRoomAt = time.Now()
			mapID, _ = roomMapID(body)
		} else {
			r.partyDungeonRoomAt = time.Time{}
		}
		r.markPartyDungeonEnteredUnsafe(time.Now())
		if packet.typ == 28 {
			// A new entry replaces the previous definition even when its body
			// is unavailable or ambiguous. Room-map notices do not replace it.
			r.partyDungeonIdentity = r.selectDungeonDefinitionIdentityUnsafe(packet)
		}
		if packet.typ == 29 {
			r.partyDungeonWorld.objectives = shared.DungeonObjectsForMap(mapID)
			foundationlog.Robotf("[PARTY_DUNGEON_ROOM] uid=%d map=%d objectives=%d\n", r.UID, mapID, len(r.partyDungeonWorld.objectives))
		}
		pkt, err := buildSendPacket(40, uint16(r.PacketID), buildFinishLoadingPayload(0, 0), r.Cipher)
		r.PacketID++
		if err != nil {
			foundationlog.Robotf("[DUNGEON_FINISH_LOADING_BUILD_ERROR] uid=%d source_type=%d err=%v\n", r.UID, packet.typ, err)
		} else if !r.sendRaw(pkt) {
			foundationlog.Robotf("[DUNGEON_FINISH_LOADING_SEND_ERROR] uid=%d source_type=%d\n", r.UID, packet.typ)
		} else if partyDebugEnabled() {
			recordPartyDebugPacket(r.UID, 0, "TX", "GAME", "DUNGEON_NOTIFY", "SENT", fmt.Sprintf("finish-loading source_type=%d command=40", packet.typ), pkt)
		}

	case 30:
		if r.State != StateRun || packet.flag != 0 || packet.size < 15 || !r.partyDungeonLoading || r.partyDungeonEnteredAt.IsZero() {
			return
		}
		if _, _, _, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti); err != nil {
			return
		}
		r.partyDungeonLoading = false
		r.partyDungeonLastAt = time.Now()
		r.partySkillNextAt = r.partyDungeonLastAt.Add(r.partySkillDelayUnsafe(r.partyDungeonLastAt))

	case 31:
		if r.State != StateRun || packet.flag != 0 || packet.size < 15 || r.partyDungeonEnteredAt.IsZero() {
			return
		}
		if _, _, _, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti); err != nil {
			return
		}
		// The server has completed the dungeon. Remaining actor deltas may name
		// summons or actors without a destroy notice; none may restart combat
		// during rewards. Keep the party transport for the normal return to town.
		r.clearPartyDungeonRuntimeUnsafe()

	case 36:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if _, _, _, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti); err == nil {
			r.confirmPartyReturnTownUnsafe()
		}

	case 22:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if body, ok := r.selectTownEntityPositionBodyUnsafe(r.Cipher, packet.data, packet.isAnti); ok {
			position, _ := r.rememberTownEntityUnsafe(body)
			r.observePartyTownReturnUnsafe(position.uniqueID)
			r.followPartyLeaderTownPositionUnsafe(position)
		}

	case 23:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if area, ok := r.selectTownEntityAreaUnsafe(r.Cipher, packet.data, packet.isAnti); ok {
			r.observePartyTownReturnUnsafe(area.uniqueID)
			r.followPartyLeaderTownAreaUnsafe(area)
		}

	case 9:
		if r.State != StateRun || packet.flag != 0 || len(packet.data) <= 15 {
			return
		}
		// A dungeon selection is only accepted once every member has reported its
		// loading screen as finished, so a member that learns about the party reports it.
		r.reportPartyLoadingUnsafe(time.Now())
		clears, source, err := partyInfoPacketClearsParty(r.Cipher, packet.data, packet.isAnti)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "PARTY_INFO_PARSE", "FAIL", err.Error(), packet.data)
			foundationlog.Robotf("[PARTY_INFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
		} else {
			r.rememberPartyRecvSourceUnsafe(source)
			if clears {
				peers := make([]string, 0, len(r.partyPeers))
				for _, peer := range r.partyPeers {
					if partyPeerIdentityKnown(peer) {
						peers = append(peers, fmt.Sprintf("s%d/a%d/u%d", peer.slot, peer.accID, peer.uniqueID))
					}
				}
				recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "PARTY_DELETE_HINT", "OBSERVED",
					fmt.Sprintf("source=%s self=s%d/a%d/u%d pending=%d peers=%s", source, r.partySelfPeer.slot,
						r.partySelfPeer.accID, r.partySelfPeer.uniqueID, r.partyPendingPeer, strings.Join(peers, ",")), packet.data)
			} else {
				r.clearPartyInviteFallbackUnsafe()
			}
		}

	case 11:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		self, peers, source, err := selectPartyIPInfoPacket(r.Cipher, packet.data, packet.isAnti, uint32(r.UID))
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "SNAPSHOT_PARSE", "FAIL", err.Error()+" candidates="+partyIPInfoDebugSummary(r.Cipher, packet.data, packet.isAnti), packet.data)
			foundationlog.Robotf("[PARTY_IPINFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d candidates=%s\n",
				r.UID, err, packet.isAnti, packet.size, partyIPInfoDebugSummary(r.Cipher, packet.data, packet.isAnti))
			return
		}
		peerAccounts := make([]string, 0, len(peers))
		for _, peer := range peers {
			peerAccounts = append(peerAccounts, fmt.Sprintf("s%d/a%d/u%d/%s>%s:%d", peer.slot, peer.accID, peer.uniqueID, peer.innerIP, peer.outerIP, peer.port))
		}
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "SNAPSHOT", "OK",
			fmt.Sprintf("source=%s self=s%d/a%d/u%d/%s>%s:%d peers=%s", source, self.slot, self.accID, self.uniqueID, self.innerIP, self.outerIP, self.port, strings.Join(peerAccounts, ",")), packet.data)
		r.rememberPartyRecvSourceUnsafe(source)
		if source == recvBodySourcePlain {
			foundationlog.Robotf("[PARTY_IPINFO_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		tracePartyIPInfo(r.UID, self, peers)
		r.partyRealtimeCandidate = [4]uint16{}
		r.partyRealtimeConfirmations = 0
		r.partyRealtimeCandidateAt = time.Time{}
		r.observePartyAccountsUnsafe(peers)
		r.setPartySelfPeerUnsafe(self)
		if (len(peers) > 0 || !r.partyDungeonInMap) && r.partyHumanObserved && r.partyRosterIsPureRobotUnsafe(r.partySelfPeer, peers) && r.startPartyRecoveryUnsafe(time.Now(), "snapshot contains only robots") && !r.partyDungeonReturnPending {
			return
		}
		r.setPartyPeersUnsafe(peers)
		r.applyPartyRealtimeIdentitiesUnsafe()
		r.ensurePartyRelayUnsafe()
		r.followCachedPartyLeaderTownPositionUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()

	case 153:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "REALTIME", "OBSERVED", partyDebugNote("type=153 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
		identities, _, err := selectPartyRealtimeInfoPacket(r.Cipher, packet.data, packet.isAnti)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "--", "GAME", "REALTIME_PARSE", "FAIL", err.Error(), nil)
			foundationlog.Robotf("[PARTY_REALTIME_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
			return
		}
		r.rememberPartyRealtimeIdentitiesUnsafe(identities)

	case 6:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
		for _, candidate := range candidates {
			if len(candidate.body) < 2 {
				continue
			}
			uniqueID := binary.LittleEndian.Uint16(candidate.body[:2])
			if uniqueID == 0 || !r.partyEntityKnownUnsafe(uniqueID) {
				continue
			}
			r.rememberPartyRecvSourceUnsafe(candidate.source)
			delete(r.townEntityPositions, uniqueID)
			break
		}

	case 173:
		if packet.flag != 0 || (r.State != StateLogin && r.State != StateRun) {
			return
		}
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "PARTY_OPTION_SOURCE", "OBSERVED", partyDebugNote("type=173 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
		if err == nil {
			optionData, ok := partyAcceptGameOptions(decData)
			if ok {
				copy(r.partyOptionData[:], optionData)
				r.partyOptionReady = true
				r.partyOptionSent = false
				r.sendPartyOptionUnsafe()
			}
		}

	case 7:
		if packet.flag != 0 || r.State != StateRun {
			return
		}
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "INVITE", "OBSERVED", partyDebugNote("type=7 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
		selected, alternate, err := selectPeerResponsePackets(r.Cipher, packet.data, packet.isAnti, r.partyRecvSource, r.partyConfirmedPeerUnsafe)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "--", "GAME", "INVITE_PARSE", "FAIL", err.Error(), nil)
			foundationlog.Robotf("[PEER_REQUEST_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
			return
		}
		data, typ, source := selected.data, selected.typ, selected.source
		recordPartyDebugPacket(r.UID, 0, "--", "GAME", "INVITE_PARSE", "OK",
			fmt.Sprintf("source=%s request_type=%d peer_unique=%d request_id=%d alternate=%t", source, typ, binary.LittleEndian.Uint16(data[0:2]), binary.LittleEndian.Uint32(data[3:7]), alternate != nil), data)
		if source == recvBodySourcePlain {
			foundationlog.Robotf("[PEER_REQUEST_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		r.rememberPartyRecvSourceUnsafe(source)
		if typ == peerRequestParty || (!r.LastTradeState && r.LastTradeID == 0) {
			uniqueID := binary.LittleEndian.Uint16(data[0:2])
			pkt, err := buildSendPacket(11, uint16(r.PacketID), data, r.Cipher)
			r.PacketID++
			if err != nil {
				recordPartyDebugPacket(r.UID, 0, "TX", "GAME", "ACCEPT", "FAIL", partyDebugNote("build request_type=%d err=%v", typ, err), nil)
				foundationlog.Robotf("[PEER_RESPONSE_BUILD_ERROR] uid=%d type=%d err=%v\n", r.UID, typ, err)
			}
			if err == nil {
				sent := r.sendRaw(pkt)
				decision := "FAIL"
				if sent {
					decision = "OK"
				}
				recordPartyDebugPacket(r.UID, 0, "TX", "GAME", "ACCEPT", decision,
					fmt.Sprintf("request_type=%d peer_unique=%d request_id=%d send_accepted=%t", typ, uniqueID, binary.LittleEndian.Uint32(data[3:7]), sent), pkt)
				if sent && typ == peerRequestTrade {
					r.LastTradeID = uniqueID
					r.LastTradeState = true
				}
				if sent && typ == peerRequestParty {
					foundationlog.Robotf("[PARTY_AUTO_ACCEPT] uid=%d peer_unique_id=%d request_id=%d\n",
						r.UID, uniqueID, binary.LittleEndian.Uint32(data[3:7]))
					r.setPartyPendingUnsafe(uniqueID)
					r.ensurePartyRelayUnsafe()
				}
				if sent {
					r.schedulePartyInviteFallbackUnsafe(selected, alternate)
				}
			}
			if typ == peerRequestTrade {
				r.invalidateTradeQuoteUnsafe()
				r.TradeMoney = 0
			}
		}
	}
}

func tracePartyIPInfo(uid uint32, self partyIPPeer, peers []partyIPPeer) {
	peerText := ""
	for _, peer := range peers {
		if peerText != "" {
			peerText += ","
		}
		peerText += fmt.Sprintf("slot%d:acc%d:uid%d:port%d", peer.slot, peer.accID, peer.uniqueID, peer.port)
	}
	foundationlog.Robotf("[PARTY_IPINFO] uid=%d self_slot=%d self_acc=%d self_unique=%d self_port=%d peers=%s\n",
		uid, self.slot, self.accID, self.uniqueID, self.port, peerText)
}

// reportPartyLoadingUnsafe tells the server this member has finished loading,
// which is what the leader's dungeon selection waits for.
func (r *RobotVo) reportPartyLoadingUnsafe(now time.Time) {
	if now.Before(r.partyLoadingReportAt) {
		return
	}
	r.partyLoadingReportAt = now.Add(2 * time.Second)
	pkt, err := buildSendPacket(40, uint16(r.PacketID), buildFinishLoadingPayload(0, 0), r.Cipher)
	if err != nil {
		return
	}
	r.PacketID++
	if r.sendRaw(pkt) {
		foundationlog.Robotf("[PARTY_LOADING_REPORTED] uid=%d\n", r.UID)
	}
}
