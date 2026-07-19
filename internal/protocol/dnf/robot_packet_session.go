package dnf

import "encoding/binary"

func (r *RobotVo) handleSessionPacketUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 {
		return
	}

	switch packet.typ {
	case 0:
		var setPos [8]byte
		pkt, err := buildSendPacket(1, uint16(r.PacketID), setPos[:], r.Cipher)
		r.PacketID++
		if err == nil {
			r.sendRaw(pkt)
		}

	case 199:
		_, _, decData, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
		if err == nil && len(decData) >= 4 {
			r.DisconReason = DisconnectReason(binary.LittleEndian.Uint32(decData[0:4]))
			if r.DisconReason != NoDisconnect {
				go r.RefishConnect()
			}
		}
	}
}
