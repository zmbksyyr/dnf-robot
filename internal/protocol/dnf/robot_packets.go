package dnf

import "encoding/binary"

type robotInboundPacket struct {
	data   []byte
	size   int
	flag   byte
	typ    uint16
	isAnti bool
}

func (r *RobotVo) parsePacket(inBuf []byte) {
	if r.State == StateStop || len(inBuf) < 7 {
		return
	}

	packet := robotInboundPacket{
		data: inBuf,
		size: len(inBuf),
		flag: inBuf[0],
		typ:  binary.LittleEndian.Uint16(inBuf[1:3]),
	}
	if packet.flag == 0 && packet.typ == 561 && packet.size > 36 {
		dec, err := r.Cipher.DecryptAnti(packet.data[19:])
		if err == nil && len(dec) >= 7 {
			packet = robotInboundPacket{
				data:   dec,
				size:   len(dec),
				flag:   dec[0],
				typ:    binary.LittleEndian.Uint16(dec[1:3]),
				isAnti: true,
			}
		}
	}

	switch packet.typ {
	case 6, 7, 9, 11, 22, 23, 28, 29, 153, 173:
		r.handlePartyPacketUnsafe(packet)
	case 1, 53, 272, 300:
		r.handleLoginPacketUnsafe(packet)
	case 13, 15, 16, 17, 88, 90, 238:
		r.handleStoreTradePacketUnsafe(packet)
	case 0, 199:
		r.handleSessionPacketUnsafe(packet)
	}
}
