package dnf

import (
	"encoding/binary"
	"fmt"
	"time"
)

func (r *RobotVo) handleStoreTradePacketUnsafe(packet robotInboundPacket) {
	switch packet.typ {
	case 238:
		if r.State != StateRun || packet.flag != 1 || r.RobotTyp != 3 {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
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

	case 88, 90:
		if r.State != StateRun || r.RobotTyp != 2 {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
		value := byte(0)
		if len(decData) > 0 {
			value = decData[0]
		}
		if packet.flag == 1 && packet.typ == 88 && err == nil && value == 1 {
			r.StoreCreated = true
		}
		if packet.flag == 1 && packet.typ == 88 && err == nil && value == 0 {
			r.StoreCreateRejected = true
		}
		if packet.flag == 1 && packet.typ == 90 && err == nil && value == 1 {
			r.StoreDisplayAck = true
		}
		storeErr := byte(0)
		if len(decData) > 1 {
			storeErr = decData[1]
		}
		if packet.flag == 1 && err == nil && value == 0 && storeErr != 0 {
			r.LastStoreError = storeErr
		}
		if packet.flag == 1 && packet.typ == 90 && err == nil && value == 0 && storeErr == 0x11 {
			r.StoreDisplayRejected = true
		}

	case 13:
		if packet.flag != 0 || (r.State != StateRun && r.State != StateLogin) {
			return
		}
		r.storeInventoryVersion++
		wasWaiting := r.IsWaitingItemList
		r.IsWaitingItemList = false
		for k := range r.InfanMap {
			delete(r.InfanMap, k)
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
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

	case 15:
		if packet.flag != 0 || r.State != StateRun {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
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
			} else if idx >= 0 && idx < 24 {
				tx := &Transaction{ItemPos: itemPos - 3, ItemId: itemID, ItemNum: itemNum, ItemType: itemType}
				if itemType == 100 || tx.ItemNum < 1 {
					tx.ItemNum = 1
				}
				r.TransactionArr[idx] = tx
			}

			r.queueTradeQuoteRefreshUnsafe()
		}

	case 16:
		if packet.flag == 0 && r.State == StateRun {
			r.clearConfirmedTradeFallbackUnsafe()
			r.invalidateTradeQuoteUnsafe()
			r.clearTradeUnsafe()
		}

	case 17:
		if packet.flag != 0 || r.State != StateRun {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
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
	}
}
