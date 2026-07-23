package dnf

import (
	"encoding/binary"
	"fmt"
	"time"

	"robot/internal/foundation/lockhub"
)

const (
	loginSelectDelay    = 3 * time.Second
	loginSelectInterval = time.Second
)

var loginSelectGate struct {
	lockhub.Locker
	next time.Time
}

func (r *RobotVo) handleLoginPacketUnsafe(packet robotInboundPacket) {
	switch packet.typ {
	case 1:
		if r.State != StateLogin {
			return
		}
		if packet.flag == 1 {
			if packet.size < 15 {
				fmt.Printf("[RobotVo] short encrypted packet uid=%d size=%d\n", r.UID, packet.size)
				r.State = StateStop
				if r.Conn != nil {
					r.Conn.Close()
					r.Conn = nil
				}
				return
			}
			_, _ = r.Cipher.DecryptLogin(packet.data[15:])
			return
		}
		if packet.flag != 0 {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
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
		}

	case 272:
		if packet.flag != 0 || r.State != StateLogin || r.NccSent {
			return
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
		if err == nil && len(decData) > 0 {
			r.NccSent = true
			pkt, err := buildSendPacket(294, 0, decData, r.Cipher)
			if err == nil {
				r.sendRaw(pkt)
			}
			r.scheduleSelectCharacUnsafe(loginSelectDelay)
		}

	case 53:
		if packet.flag == 0 && r.State == StateLogin && !r.SelectCharacSent {
			r.CharacListReady = true
		}

	case 300:
		if packet.flag != 0 || r.State != StateLogin {
			return
		}
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
	}
}

func (r *RobotVo) scheduleSelectCharacUnsafe(minDelay time.Duration) {
	if r.State != StateLogin || !r.NccSent || r.SelectCharacSent || r.SelectCharacQueued {
		return
	}
	r.SelectCharacQueued = true
	time.AfterFunc(minDelay, r.trySendQueuedSelectCharac)
}

func (r *RobotVo) trySendQueuedSelectCharac() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateLogin || !r.NccSent || r.SelectCharacSent {
		r.SelectCharacQueued = false
		return
	}
	if delay := claimLoginSelectSlot(); delay > 0 {
		time.AfterFunc(delay, r.trySendQueuedSelectCharac)
		return
	}
	r.SelectCharacQueued = false
	r.sendSelectCharacUnsafe("after throttled type=272")
}

func claimLoginSelectSlot() time.Duration {
	now := time.Now()
	loginSelectGate.Lock()
	defer loginSelectGate.Unlock()
	if now.Before(loginSelectGate.next) {
		return time.Until(loginSelectGate.next)
	}
	loginSelectGate.next = now.Add(loginSelectInterval)
	return 0
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
