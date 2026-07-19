package dnf

import (
	"encoding/binary"
	"fmt"
	"time"
)

func (r *RobotVo) parsePacket(inBuf []byte) {
	if r.State == StateStop {
		return
	}
	if len(inBuf) < 7 {
		return
	}

	pInBuf := inBuf
	dInSize := len(inBuf)
	packetFlag := pInBuf[0]
	packetType := binary.LittleEndian.Uint16(pInBuf[1:3])
	_ = binary.LittleEndian.Uint32(pInBuf[3:7])
	isAnti := false

	if packetFlag == 0 && packetType == 561 && dInSize > 36 {
		dec, err := r.Cipher.DecryptAnti(pInBuf[19:])
		if err == nil && len(dec) >= 7 {
			pInBuf = dec
			dInSize = len(dec)
			packetFlag = pInBuf[0]
			packetType = binary.LittleEndian.Uint16(pInBuf[1:3])
			_ = binary.LittleEndian.Uint32(pInBuf[3:7])
			isAnti = true
		}
	}

	if r.State == StateRun && packetFlag == 0 && dInSize >= 15 && (packetType == 28 || packetType == 29) {
		pkt, err := buildSendPacket(40, uint16(r.PacketID), buildFinishLoadingPayload(0, 0), r.Cipher)
		r.PacketID++
		if err != nil {
			fmt.Printf("[DUNGEON_FINISH_LOADING_BUILD_ERROR] uid=%d source_type=%d err=%v\n", r.UID, packetType, err)
		} else if !r.sendRaw(pkt) {
			fmt.Printf("[DUNGEON_FINISH_LOADING_SEND_ERROR] uid=%d source_type=%d\n", r.UID, packetType)
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 22 {
		if body, ok := r.selectTownEntityPositionBodyUnsafe(r.Cipher, pInBuf, isAnti); ok {
			position, _ := r.rememberTownEntityUnsafe(body)
			r.followPartyLeaderTownPositionUnsafe(position)
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 23 {
		if area, ok := r.selectTownEntityAreaUnsafe(r.Cipher, pInBuf, isAnti); ok {
			r.followPartyLeaderTownAreaUnsafe(area)
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 9 && len(pInBuf) > 15 {
		clears, source, err := partyInfoPacketClearsParty(r.Cipher, pInBuf, isAnti)
		if err != nil {
			fmt.Printf("[PARTY_INFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, isAnti, dInSize)
		} else {
			r.rememberPartyRecvSourceUnsafe(source)
			if clears && source == recvBodySourcePlain {
				fmt.Printf("[PARTY_INFO_PLAIN] uid=%d size=%d\n", r.UID, dInSize)
			}
			if clears {
				r.clearPartyUnsafe()
			} else {
				r.clearPartyInviteFallbackUnsafe()
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 11 {
		self, peers, source, err := selectPartyIPInfoPacket(r.Cipher, pInBuf, isAnti, uint32(r.UID))
		if err != nil {
			fmt.Printf("[PARTY_IPINFO_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, isAnti, dInSize)
			return
		}
		r.rememberPartyRecvSourceUnsafe(source)
		if source == recvBodySourcePlain {
			fmt.Printf("[PARTY_IPINFO_PLAIN] uid=%d size=%d\n", r.UID, dInSize)
		}
		tracePartyIPInfo(r.UID, self, peers)
		r.partySelfPeer = self
		r.setPartyPeersUnsafe(peers)
		r.ensurePartyRelayUnsafe()
		r.followCachedPartyLeaderTownPositionUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 6 {
		candidates, _ := recvBodyCandidates(r.Cipher, pInBuf, isAnti)
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
		return
	}

	// Handle flag=1 (encrypted server-to-client) packets
	if packetFlag == 1 && packetType == 1 && r.State == StateLogin {
		if dInSize < 15 {
			fmt.Printf("[RobotVo] short encrypted packet uid=%d size=%d\n", r.UID, dInSize)
			r.State = StateStop
			if r.Conn != nil {
				r.Conn.Close()
				r.Conn = nil
			}
			return
		}
		encryptedData := pInBuf[15:]
		_, _ = r.Cipher.DecryptLogin(encryptedData)
	}

	if packetFlag == 0 && packetType == 272 && r.State == StateLogin && !r.NccSent {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) > 0 {
			r.NccSent = true
			// Send NCC response (type=294) first
			pkt, err := buildSendPacket(294, 0, decData, r.Cipher)
			if err == nil {
				r.sendRaw(pkt)
			}
			r.sendSelectCharacUnsafe("after type=272")
		}
	}

	if packetFlag == 0 && packetType == 0 {
		var setPos [8]byte
		pkt, err := buildSendPacket(1, uint16(r.PacketID), setPos[:], r.Cipher)
		r.PacketID++
		if err == nil {
			r.sendRaw(pkt)
		}
	}

	if packetFlag == 0 && packetType == 199 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 4 {
			r.DisconReason = DisconnectReason(binary.LittleEndian.Uint32(decData[0:4]))
			if r.DisconReason != NoDisconnect {
				go r.RefishConnect()
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 173 && (r.State == StateLogin || r.State == StateRun) {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			optionData, ok := partyAcceptGameOptions(decData)
			if ok {
				copy(r.partyOptionData[:], optionData)
				r.partyOptionReady = true
				r.partyOptionSent = false
				r.sendPartyOptionUnsafe()
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 1 && packetType == 238 && r.RobotTyp == 3 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			if len(decData) > 0 && decData[0] == 1 {
				r.DisjointDirectAck = true
				r.DisjointActive = true
				r.LastDisjointError = 0
				fmt.Printf("[DISJOINT_DIRECT_ACK] uid=%d payload=%x\n", r.UID, decData)
			} else if len(decData) >= 2 && decData[0] == 0 {
				r.DisjointDirectAck = false
				r.DisjointActive = false
				r.LastDisjointError = decData[1]
				fmt.Printf("[DISJOINT_238_ERROR] uid=%d error=%d payload=%x\n", r.UID, r.LastDisjointError, decData)
			}
		}
		return
	}

	if r.State == StateRun && (packetType == 88 || packetType == 90) && r.RobotTyp == 2 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		value := byte(0)
		if len(decData) > 0 {
			value = decData[0]
		}
		if packetFlag == 1 && packetType == 88 && err == nil && value == 1 {
			r.StoreCreated = true
		}
		if packetFlag == 1 && packetType == 88 && err == nil && value == 0 {
			r.StoreCreateRejected = true
		}
		if packetFlag == 1 && packetType == 90 && err == nil && value == 1 {
			r.StoreDisplayAck = true
		}
		storeErr := byte(0)
		if len(decData) > 1 {
			storeErr = decData[1]
		}
		if packetFlag == 1 && err == nil && value == 0 && storeErr != 0 {
			r.LastStoreError = storeErr
		}
		if packetFlag == 1 && packetType == 90 && err == nil && value == 0 && storeErr == 0x11 {
			r.StoreDisplayRejected = true
		}
	}

	if packetFlag == 0 && packetType == 13 && (r.State == StateRun || r.State == StateLogin) {
		r.storeInventoryVersion++
		wasWaiting := r.IsWaitingItemList
		r.IsWaitingItemList = false
		for k := range r.InfanMap {
			delete(r.InfanMap, k)
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 5 {
			itemNumber := binary.LittleEndian.Uint16(decData[3:5])
			pBuf := decData[5:]
			for i := uint16(0); i < itemNumber; i++ {
				if len(pBuf) < 25 {
					break
				}
				itemID := int32(binary.LittleEndian.Uint32(pBuf[2:6]))
				itemPos := int16(binary.LittleEndian.Uint16(pBuf[0:2]))
				itemNum := int32(binary.LittleEndian.Uint32(pBuf[6:10]))
				pBuf = pBuf[25:]
				r.InfanMap[int(itemID)] = Transaction{ItemPos: itemPos, ItemId: itemID, ItemNum: itemNum}
			}
		}
		if wasWaiting {
			if r.PrepareStoreAfterItemList {
				r.PrepareStoreAfterItemList = false
				go func() {
					r.CreatePrivateStore()
					deadline := time.Now().Add(4 * time.Second)
					for time.Now().Before(deadline) {
						snap := r.Snapshot()
						if snap.StoreCreated || snap.State != StateRun {
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
					if snap := r.Snapshot(); snap.State == StateRun {
						r.GetDbDataAndCompleteDisplay()
					}
				}()
			} else {
				go r.GetDbDataAndCompleteDisplay()
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 15 && r.State == StateRun {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 15 {
			if r.StoreDisplaySent && r.RobotTyp == 2 && !r.StoreDisplayAck {
				r.StoreDisplayAck = true
			}
			itemPos := int16(binary.LittleEndian.Uint16(decData[0:2]))
			itemID := int32(binary.LittleEndian.Uint32(decData[2:6]))
			itemNum := int32(binary.LittleEndian.Uint32(decData[6:10]))
			itemType := int32(binary.LittleEndian.Uint32(decData[11:15]))
			r.clearConfirmedTradeFallbackUnsafe()

			idx := int(itemPos) - 3
			if itemID < 0 {
				if idx >= 0 && idx < 24 {
					r.TransactionArr[idx] = nil
				}
			} else {
				if idx >= 0 && idx < 24 {
					tx := &Transaction{ItemPos: itemPos - 3, ItemId: itemID, ItemNum: itemNum, ItemType: itemType}
					if itemType == 100 || tx.ItemNum < 1 {
						tx.ItemNum = 1
					}
					r.TransactionArr[idx] = tx
				}
			}

			r.queueTradeQuoteRefreshUnsafe()
		}
		return
	}

	if packetFlag == 0 && packetType == 7 && r.State == StateRun {
		selected, alternate, err := selectPeerResponsePackets(r.Cipher, pInBuf, isAnti, r.partyRecvSource, r.partyConfirmedPeerUnsafe)
		if err != nil {
			fmt.Printf("[PEER_REQUEST_PARSE_ERROR] uid=%d err=%v anti=%t size=%d\n", r.UID, err, isAnti, dInSize)
			return
		}
		data, typ, source := selected.data, selected.typ, selected.source
		if source == recvBodySourcePlain {
			fmt.Printf("[PEER_REQUEST_PLAIN] uid=%d size=%d\n", r.UID, dInSize)
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
		return
	}

	if packetFlag == 0 && packetType == 16 && r.State == StateRun {
		r.clearConfirmedTradeFallbackUnsafe()
		r.invalidateTradeQuoteUnsafe()
		r.clearTradeUnsafe()
		return
	}

	if packetFlag == 0 && packetType == 17 && r.State == StateRun {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 3 {
			r.clearConfirmedTradeFallbackUnsafe()
			uniqueID := binary.LittleEndian.Uint16(decData[0:2])
			state := decData[2]
			if uniqueID == r.LastTradeID && state == 1 {
				var data [8]byte
				data[0] = 1
				pkt, err := buildSendPacket(26, uint16(r.PacketID), data[:], r.Cipher)
				r.PacketID++
				if err == nil {
					r.sendRaw(pkt)
				}
			}
			if uniqueID != r.LastTradeID && state == 1 {
				var data [8]byte
				data[0] = 3
				pkt, err := buildSendPacket(26, uint16(r.PacketID), data[:], r.Cipher)
				r.PacketID++
				if err == nil {
					r.sendRaw(pkt)
				}
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 1 && r.State == StateLogin {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 334 {
			errInit := r.Cipher.Initialize(decData[:334])
			if errInit == nil {
				const loginBodySize = 400
				if 8+int(r.TokenSize) > loginBodySize {
					r.State = StateStop
					if r.Conn != nil {
						r.Conn.Close()
						r.Conn = nil
					}
					return
				}
				for i := 0; i < 4; i++ {
					r.loginReal[i] = 0
				}
				binary.LittleEndian.PutUint32(r.loginReal[4:8], r.TokenSize)
				copy(r.loginReal[8:], r.Token[:r.TokenSize])
				copy(r.loginReal[8+r.TokenSize:], r.loginEnd[:])
				var body [400]byte
				copy(body[:], r.loginReal[:loginBodySize])
				pkt, err := buildSendPacket(1, 0, body[:], r.Cipher)
				if err == nil {
					r.sendRaw(pkt)
				} else {
					fmt.Printf("[RobotVo] LOGIN SEND ERR: %v\n", err)
				}
			}
			return
		}
		return
	}

	if packetFlag == 0 && packetType == 53 && r.State == StateLogin && !r.SelectCharacSent {
		r.sendSelectCharacUnsafe("after type=53")
		return
	}

	if packetFlag == 0 && packetType == 300 && r.State == StateLogin {
		pkt, err := buildSendPacket(37, 19, r.setPos[:], r.Cipher)
		if err == nil {
			r.sendRaw(pkt)
		}

		r.setArea[0] = r.CurVillage
		r.setArea[1] = r.CurArea
		binary.LittleEndian.PutUint16(r.setArea[2:4], r.CurX)
		binary.LittleEndian.PutUint16(r.setArea[4:6], r.CurY)
		r.setArea[7] = 0x01
		binary.LittleEndian.PutUint16(r.setArea[8:10], uint16(r.CurVillage))
		binary.LittleEndian.PutUint16(r.setArea[10:12], uint16(dnfGateAreaForVillage(int(r.CurVillage))))
		pkt, err = buildSendPacket(38, 26, r.setArea[:], r.Cipher)
		if err == nil {
			r.sendRaw(pkt)
		}

		binary.LittleEndian.PutUint16(r.setPosStart[0:2], r.CurX)
		binary.LittleEndian.PutUint16(r.setPosStart[2:4], r.CurY)
		pkt, err = buildSendPacket(37, 27, r.setPosStart[:], r.Cipher)
		if err == nil {
			if r.sendRaw(pkt) {
				r.PacketID = 29
				r.State = StateRun
				r.ConnCount = 0
				r.sendNATInfoUnsafe()
				r.sendPartyOptionUnsafe()
				if r.RunStartTime == 0 {
					r.RunStartTime = uint32(time.Now().Unix())
				}
			}
		}
		return
	}
}

func (r *RobotVo) sendSelectCharacUnsafe(_ string) bool {
	if r.State != StateLogin || r.SelectCharacSent {
		return false
	}
	r.selectCharac[0] = byte(r.CID)
	pkt, err := buildSendPacket(4, 12, r.selectCharac[:], r.Cipher)
	if err != nil {
		return false
	}
	if !r.sendRaw(pkt) {
		return false
	}
	r.SelectCharacSent = true
	return true
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
