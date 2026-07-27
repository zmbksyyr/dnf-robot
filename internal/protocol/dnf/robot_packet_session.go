package dnf

import (
	"encoding/binary"
	"fmt"
)

func (r *RobotVo) handleSessionPacketUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 {
		return
	}

	switch packet.typ {
	case 0:
		var checksums [8]byte
		pkt, err := buildSendPacket(0, uint16(r.PacketID), checksums[:], r.Cipher)
		if err != nil {
			fmt.Printf("[SESSION_CHECK_RESPONSE_ERROR] uid=%d stage=build err=%v\n", r.UID, err)
			return
		}
		if !r.sendRaw(pkt) {
			fmt.Printf("[SESSION_CHECK_RESPONSE_ERROR] uid=%d stage=send\n", r.UID)
			return
		}
		r.PacketID++

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
