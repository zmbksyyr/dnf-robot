package dnf

import (
	"encoding/binary"
	"robot/internal/shared"
	"time"

	foundationlog "robot/internal/foundation/log"
)

func (r *RobotVo) handleLoginPacketUnsafe(packet robotInboundPacket) {
	switch packet.typ {
	case 1:
		if r.State != StateLogin {
			return
		}
		if packet.flag == 1 {
			if packet.size < 15 {
				foundationlog.Robotf("[RobotVo] short encrypted packet uid=%d size=%d\n", r.UID, packet.size)
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
					foundationlog.Robotf("[RobotVo] LOGIN SEND ERR: %v\n", err)
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
			r.sendSelectCharacUnsafe("after type=272")
		}

	case 53:
		if packet.flag == 0 && r.State == StateLogin && !r.SelectCharacSent {
			r.sendSelectCharacUnsafe("after type=53")
		}

	case 300:
		if packet.flag != 0 || r.State != StateLogin {
			return
		}
		if !shared.GenericAreaAllowed(r.GuildID, int(r.CurVillage)) {
			foundationlog.Robotf("[AreaPolicy] blocked unsafe login uid=%d cid=%d guild_id=%d village=%d area=%d\n",
				r.UID, r.CID, r.GuildID, r.CurVillage, r.CurArea)
			r.closeOutUnsafe()
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
		// The reference reads the origin pair right after the z byte: previous
		// village at offset 7 and previous area at offset 9, both as shorts.
		binary.LittleEndian.PutUint16(r.setArea[7:9], uint16(r.CurVillage))
		binary.LittleEndian.PutUint16(r.setArea[9:11], uint16(r.GateArea))
		pkt, err = buildSendPacket(38, 26, r.setArea[:], r.Cipher)
		if err == nil {
			r.sendRaw(pkt)
		}

		binary.LittleEndian.PutUint16(r.setPosStart[0:2], r.CurX)
		binary.LittleEndian.PutUint16(r.setPosStart[2:4], r.CurY)
		pkt, err = buildSendPacket(37, 27, r.setPosStart[:], r.Cipher)
		if err == nil {
			if r.sendRaw(pkt) {
				// The server keeps the user out of the dungeon-capable state
				// until the client reports its loading screen as finished; without
				// this a party member can never satisfy the all-members-loaded
				// check that a dungeon selection performs.
				if finish, finishErr := buildSendPacket(40, 28, buildFinishLoadingPayload(0, 0), r.Cipher); finishErr == nil {
					r.sendRaw(finish)
				}
				r.PacketID = 29
				r.State = StateRun
				r.ConnCount = 0
				r.sendNATInfoUnsafe()
				r.sendPartyOptionUnsafe()
				if r.RunStartTime == 0 {
					r.RunStartTime = uint32(time.Now().Unix())
				}
				r.runDisjointAfterLoginUnsafe()
				if r.partyRecoveryResumePending {
					r.partyRecoveryResumePending = false
					recordPartyDebugPacket(r.UID, 0, "--", "CORE", "PARTY_NORMAL", "OK", "running state restored", nil)
				}
			}
		}
	}
}

func (r *RobotVo) sendSelectCharacUnsafe(_ string) bool {
	if r.State != StateLogin || r.SelectCharacSent {
		return false
	}
	// CMD 4/12 selects a character-list slot. The database charac_no stored in
	// CID is runtime identity and must never be narrowed into this byte.
	r.selectCharac[0] = r.CharacterSlot
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
