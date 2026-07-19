package dnf

import (
	"encoding/binary"
	"fmt"
)

func (r *RobotVo) handlePartyPacketUnsafe(packet robotInboundPacket) {
	switch packet.typ {
	case 28, 29:
		if r.State != StateRun || packet.flag != 0 || packet.size < 15 {
			return
		}
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
			position, _ := r.rememberTownEntityUnsafe(body)
			r.followPartyLeaderTownPositionUnsafe(position)
		}

	case 23:
		if r.State != StateRun || packet.flag != 0 {
			return
		}
		if area, ok := r.selectTownEntityAreaUnsafe(r.Cipher, packet.data, packet.isAnti); ok {
			r.followPartyLeaderTownAreaUnsafe(area)
		}

	case 9:
		if r.State != StateRun || packet.flag != 0 || len(packet.data) <= 15 {
			return
		}
		clears, source, err := partyInfoPacketClearsParty(r.Cipher, packet.data, packet.isAnti)
		if err != nil {
			fmt.Printf("[PARTY_INFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
		} else {
			r.rememberPartyRecvSourceUnsafe(source)
			if clears && source == recvBodySourcePlain {
				fmt.Printf("[PARTY_INFO_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
			}
			if clears {
				r.clearPartyUnsafe()
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
			fmt.Printf("[PARTY_IPINFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
			return
		}
		r.rememberPartyRecvSourceUnsafe(source)
		if source == recvBodySourcePlain {
			fmt.Printf("[PARTY_IPINFO_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		tracePartyIPInfo(r.UID, self, peers)
		r.partySelfPeer = self
		r.setPartyPeersUnsafe(peers)
		r.ensurePartyRelayUnsafe()
		r.followCachedPartyLeaderTownPositionUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()

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
		selected, alternate, err := selectPeerResponsePackets(r.Cipher, packet.data, packet.isAnti, r.partyRecvSource, r.partyConfirmedPeerUnsafe)
		if err != nil {
			fmt.Printf("[PEER_REQUEST_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, packet.isAnti, packet.size)
			return
		}
		data, typ, source := selected.data, selected.typ, selected.source
		if source == recvBodySourcePlain {
			fmt.Printf("[PEER_REQUEST_PLAIN] uid=%d size=%d\n", r.UID, packet.size)
		}
		r.rememberPartyRecvSourceUnsafe(source)
		if typ == peerRequestParty || (!r.LastTradeState && r.LastTradeID == 0) {
			uniqueID := binary.LittleEndian.Uint16(data[0:2])
			pkt, err := buildSendPacket(11, uint16(r.PacketID), data, r.Cipher)
			r.PacketID++
			if err != nil {
				fmt.Printf("[PEER_RESPONSE_BUILD_ERROR] uid=%d type=%d err=%v\n", r.UID, typ, err)
			}
			if err == nil {
				sent := r.sendRaw(pkt)
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
