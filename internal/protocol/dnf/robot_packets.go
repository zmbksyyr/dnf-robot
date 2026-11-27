package dnf

import (
	"encoding/binary"
	"fmt"
	"os"

	foundationlog "robot/internal/foundation/log"
)

type robotInboundPacket struct {
	data   []byte
	size   int
	flag   byte
	typ    uint16
	isAnti bool
}

func (r *RobotVo) parsePacket(inBuf []byte) {
	if r.State == StateStop || r.State == StateWrong || len(inBuf) < 7 {
		return
	}

	packet := robotInboundPacket{
		data: inBuf,
		size: len(inBuf),
		flag: inBuf[0],
		typ:  binary.LittleEndian.Uint16(inBuf[1:3]),
	}
	if os.Getenv("DNF_TRACE_RAW_ALL") == "1" && packet.typ == 561 {
		fmt.Fprintf(os.Stderr, "[RX] typ=561 raw=%x\n", packet.data)
	}
	if packet.flag == 0 && packet.typ == 561 && packet.size > 36 {
		dec, err := r.Cipher.DecryptAnti(packet.data[19:])
		if err != nil && partyDebugEnabled() {
			recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "DUNGEON_NOTIFY", "FAIL", "anti-decrypt: "+err.Error(), packet.data)
			foundationlog.Robotf("[PARTY_DUNGEON_NOTIFY] uid=%d type=561 size=%d error=%v\n", r.UID, packet.size, err)
		}
		if os.Getenv("DNF_TRACE_RX") == "1" {
			if err != nil {
				fmt.Fprintf(os.Stderr, "[RX] anti decrypt failed size=%d err=%v\n", packet.size, err)
			} else if len(dec) >= 7 {
				fmt.Fprintf(os.Stderr, "[RX] anti inner size=%d typ=%d\n", len(dec), int(binary.LittleEndian.Uint16(dec[1:3])))
			}
		}
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
	if partyDebugEnabled() && packet.flag == 0 && packet.typ >= 27 && packet.typ <= 42 {
		note := fmt.Sprintf("type=%d anti=%t size=%d", packet.typ, packet.isAnti, packet.size)
		recordPartyDebugPacket(r.UID, 0, "RX", "GAME", "DUNGEON_NOTIFY", "OBSERVED", note, packet.data)
		foundationlog.Robotf("[PARTY_DUNGEON_NOTIFY] uid=%d %s\n", r.UID, note)
	}
	if os.Getenv("DNF_TRACE_RX") == "1" {
		line := fmt.Sprintf("[RX] typ=%d size=%d", packet.typ, packet.size)
		if packet.typ == 29 && os.Getenv("DNF_TRACE_RAW29") == "1" {
			line += fmt.Sprintf(" raw=%x", packet.data)
		}
		if packet.typ == 16 || packet.typ == 48 || packet.typ == 29 || packet.typ == 100 || packet.typ == 12 || packet.typ == 22 || packet.typ == 23 || os.Getenv("DNF_TRACE_RAW_ALL") == "1" {
			if _, _, body, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti); err == nil {
				line += fmt.Sprintf(" body=%x", body)
			}
		}
		fmt.Fprintln(os.Stderr, line)
	}
	switch packet.typ {
	case 2, 4, 33, 37:
		r.handleCharacterInfoUnsafe(packet)
	case 19:
		r.handleLearnedSkillsUnsafe(packet)
	case 7:
		if !r.handleReturnToSelectPacketUnsafe(packet) {
			r.handlePartyPacketUnsafe(packet)
		}
	case 6, 9, 11, 22, 23, 28, 29, 30, 31, 36, 153, 173:
		r.handlePartyPacketUnsafe(packet)
	case guildInviteNotification:
		r.handleGuildPacketUnsafe(packet)
	case 1, 53, 272, 300:
		r.handleLoginPacketUnsafe(packet)
	case 13, 14, 15, 16, 17, 18, 88, 90, 238:
		r.handleStoreTradePacketUnsafe(packet)
	case 0, 199:
		r.handleSessionPacketUnsafe(packet)
	}
}
