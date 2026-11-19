package dnf

import (
	"encoding/binary"
)

// Legacy clients put the same actor/state/data/token tuple directly in command
// 5. Newer clients wrap it in command 81. Select the direct form only after a
// valid command 5 for the leader has been observed in this dungeon session.
func buildPartyLegacySkillStateBody(uniqueID uint16, state byte, stateData []byte, token uint16) []byte {
	payload := make([]byte, 9+len(stateData))
	payload[0], payload[1] = 0x11, 0x01
	binary.LittleEndian.PutUint16(payload[2:4], uniqueID)
	payload[4] = state
	binary.LittleEndian.PutUint16(payload[5:7], uint16(len(stateData)))
	copy(payload[7:], stateData)
	binary.LittleEndian.PutUint16(payload[7+len(stateData):], token)
	body := make([]byte, 7+len(payload))
	body[0], body[1] = 2, 5
	checksum := partyPayloadChecksum(payload)
	body[3] = checksum[0]
	body[4], body[5], body[6] = byte(token), byte(token>>8), byte(uniqueID)^state
	for i, value := range payload {
		body[7+i] = value ^ 0x7e
	}
	return body
}

func partyDungeonFrameUsesLegacySkills(frame []byte, leaderID uint16) bool {
	if len(frame) < 9 || len(frame) != 9+int(binary.LittleEndian.Uint16(frame[5:7])) {
		return false
	}
	body := frame[9:]
	if frame[0] == 2 {
		return validPartyLegacySkillBody(body, leaderID)
	}
	if frame[0] != 1 {
		return false
	}
	found := false
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size == 0 || size > len(body) {
			return false
		}
		found = validPartyLegacySkillBody(body[:size], leaderID) || found
		body = body[size:]
	}
	return found && len(body) == 0
}

func validPartyLegacySkillBody(body []byte, leaderID uint16) bool {
	if len(body) < 16 || body[0] != 2 || binary.LittleEndian.Uint16(body[1:3]) != 5 {
		return false
	}
	payload := make([]byte, len(body)-7)
	for i, value := range body[7:] {
		payload[i] = value ^ 0x7e
	}
	if payload[0] != 0x11 || payload[1] != 1 || leaderID == 0 || binary.LittleEndian.Uint16(payload[2:4]) != leaderID || len(payload) != 9+int(binary.LittleEndian.Uint16(payload[5:7])) {
		return false
	}
	checksum := partyPayloadChecksum(payload)
	return body[3] == checksum[0]
}

// The server's active skill tree is authoritative, including changes made
// during a live session. Each entry is slot/index/level/command-length/command.
func parsePartyLearnedSkills(body []byte) ([256]byte, bool) {
	var levels [256]byte
	if len(body) < 3 || body[2] > 204 {
		return levels, false
	}
	count := int(body[2])
	body = body[3:]
	var slots [204]bool
	for i := 0; i < count; i++ {
		if len(body) < 4 || int(body[0]) >= len(slots) || slots[body[0]] || body[1] == 0 || levels[body[1]] != 0 || body[2] == 0 || int(body[3]) > len(body)-4 {
			return [256]byte{}, false
		}
		slots[body[0]] = true
		levels[body[1]] = body[2]
		body = body[4+int(body[3]):]
	}
	return levels, true
}

func (r *RobotVo) handleLearnedSkillsUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 || (r.State != StateLogin && r.State != StateRun) {
		return
	}
	_, _, body, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
	if err != nil {
		return
	}
	levels, ok := parsePartyLearnedSkills(body)
	if !ok {
		return
	}
	r.partySkillLevels = levels
	r.partySkillInfoKnown = true
	r.resetPartySkillProfileUnsafe()
}
