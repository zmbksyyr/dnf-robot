package dnf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func (r *RobotVo) handlePartyPacketUnsafe(packet robotInboundPacket) {
	switch packet.typ {
	case 28, 29:
		if r.State != StateRun || packet.flag != 0 || packet.size < 15 {
			return
		}
		r.markPartyDungeonEnteredUnsafe(time.Now())
		pkt, err := buildSendPacket(40, uint16(r.PacketID), buildFinishLoadingPayload(0, 0), r.Cipher)
		r.PacketID++
		if err != nil {
			fmt.Printf("[DUNGEON_FINISH_LOADING_BUILD_ERROR] uid=%d source_type=%d err=%v\n", r.UID, packet.typ, err)
		} else if !r.sendRaw(pkt) {
			fmt.Printf("[DUNGEON_FINISH_LOADING_SEND_ERROR] uid=%d source_type=%d\n", r.UID, packet.typ)
		}

	case 22:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if body, ok := r.selectTownEntityPositionBodyUnsafe(r.Cipher, packet.data, packet.isAnti); ok {
			r.clearPartyDungeonRuntimeUnsafe()
			position, _ := r.rememberTownEntityUnsafe(body)
			r.followPartyLeaderTownPositionUnsafe(position)
		}

	case 23:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if area, ok := r.selectTownEntityAreaUnsafe(r.Cipher, packet.data, packet.isAnti); ok {
			r.clearPartyDungeonRuntimeUnsafe()
			r.followPartyLeaderTownAreaUnsafe(area)
		}

	case 9:
		if r.State != StateRun || packet.flag != 0 || len(packet.data) <= 15 {
			return
		}
		clears, source, err := partyInfoPacketClearsParty(r.Cipher, packet.data, packet.isAnti)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "PARTY_INFO_PARSE", "FAIL", err.Error(), packet.data)
			fmt.Printf("[PARTY_INFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
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
			fmt.Printf("[PARTY_IPINFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d candidates=%s\n",
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
			fmt.Printf("[PARTY_IPINFO_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		tracePartyIPInfo(r.UID, self, peers)
		r.partyRealtimeCandidate = [4]uint16{}
		r.partyRealtimeConfirmations = 0
		r.partyRealtimeCandidateAt = time.Time{}
		r.observePartyAccountsUnsafe(peers)
		r.setPartySelfPeerUnsafe(self)
		if r.partyHumanObserved && r.partyRosterIsPureRobotUnsafe(r.partySelfPeer, peers) && r.startPartyRecoveryUnsafe(time.Now(), "snapshot contains only robots") {
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
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "REALTIME", "OBSERVED", fmt.Sprintf("type=153 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
		identities, _, err := selectPartyRealtimeInfoPacket(r.Cipher, packet.data, packet.isAnti)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "--", "GAME", "REALTIME_PARSE", "FAIL", err.Error(), nil)
			fmt.Printf("[PARTY_REALTIME_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
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
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "PARTY_OPTION_SOURCE", "OBSERVED", fmt.Sprintf("type=173 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
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
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "INVITE", "OBSERVED", fmt.Sprintf("type=7 anti=%t size=%d", packet.isAnti, packet.size), packet.data)
		selected, alternate, err := selectPeerResponsePackets(r.Cipher, packet.data, packet.isAnti, r.partyRecvSource, r.partyConfirmedPeerUnsafe)
		if err != nil {
			recordPartyDebugPacket(r.UID, 0, "--", "GAME", "INVITE_PARSE", "FAIL", err.Error(), nil)
			fmt.Printf("[PEER_REQUEST_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
			return
		}
		data, typ, source := selected.data, selected.typ, selected.source
		recordPartyDebugPacket(r.UID, 0, "--", "GAME", "INVITE_PARSE", "OK",
			fmt.Sprintf("source=%s request_type=%d peer_unique=%d request_id=%d alternate=%t", source, typ, binary.LittleEndian.Uint16(data[0:2]), binary.LittleEndian.Uint32(data[3:7]), alternate != nil), data)
		if source == recvBodySourcePlain {
			fmt.Printf("[PEER_REQUEST_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		r.rememberPartyRecvSourceUnsafe(source)
		if typ == peerRequestParty || (!r.LastTradeState && r.LastTradeID == 0) {
			uniqueID := binary.LittleEndian.Uint16(data[0:2])
			pkt, err := buildSendPacket(11, uint16(r.PacketID), data, r.Cipher)
			r.PacketID++
			if err != nil {
				recordPartyDebugPacket(r.UID, 0, "TX", "GAME", "ACCEPT", "FAIL", fmt.Sprintf("build request_type=%d err=%v", typ, err), nil)
				fmt.Printf("[PEER_RESPONSE_BUILD_ERROR] uid=%d type=%d err=%v\n", r.UID, typ, err)
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
					fmt.Printf("[PARTY_AUTO_ACCEPT] uid=%d peer_unique_id=%d request_id=%d\n",
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
	fmt.Printf("[PARTY_IPINFO] uid=%d self_slot=%d self_acc=%d self_unique=%d self_port=%d peers=%s\n",
		uid, self.slot, self.accID, self.uniqueID, self.port, peerText)
}
