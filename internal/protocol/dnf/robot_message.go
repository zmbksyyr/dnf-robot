package dnf

import (
	"encoding/binary"
	"strconv"
)

func (r *RobotVo) SendMsg(buf []byte) bool {
	return r.sendRaw(buf)
}

func (r *RobotVo) SendPublicMessage(msgType int, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	msgStr := string(msg)
	sendMsgType := byte(msgType)
	if sendMsgType == 0 {
		sendMsgType = 0x03
	}
	if basePos := findSubstring(msgStr, "ext("); basePos >= 0 {
		fPos := basePos + 4
		if endP := findSubstring(msgStr[fPos:], ")"); endP >= 0 {
			if t, err := strconv.Atoi(msgStr[fPos : fPos+endP]); err == nil {
				sendMsgType = byte(t)
			}
			msg = msg[:basePos]
		}
	}

	r.sendPublicMessagePacket(sendMsgType, 0x24, msg)
}

func (r *RobotVo) sendPublicMessagePacket(msgType, flag byte, msg []byte) {
	realSize := 1 + 2 + 4 + 4 + len(msg)
	alinSize := alignTo16(realSize)
	data := make([]byte, alinSize)
	data[0] = msgType
	data[1] = flag
	binary.LittleEndian.PutUint32(data[7:11], uint32(len(msg)))
	copy(data[11:], msg)
	pkt, err := buildSendPacket(17, uint16(r.PacketID), data, r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
