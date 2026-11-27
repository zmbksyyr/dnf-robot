package dnf

import (
	"encoding/binary"

	foundationlog "robot/internal/foundation/log"
)

// This is the server identity's unsigned level byte. Native owner stat3 also
// passes through attribute refresh; this snapshot alone does not resolve it.
type characterLevelInfo struct {
	known    bool
	uniqueID uint16
	level    byte
}

func parseInflatedCharacterLevelInfo(body []byte, selfUnique uint16) (characterLevelInfo, bool) {
	// Mode 0: count:u16, unique:u16, name length:u32, name bytes,
	// job:u8, growth:u8, level:u8, then independent variable town data.
	// Only a single self record is supported; mode 2 is the character list.
	if selfUnique == 0 || len(body) < 9 || body[0] != 0 ||
		binary.LittleEndian.Uint16(body[1:3]) != 1 || binary.LittleEndian.Uint16(body[3:5]) != selfUnique {
		return characterLevelInfo{}, false
	}
	nameSize := binary.LittleEndian.Uint32(body[5:9])
	// The native 31-byte name buffer only consumes lengths below 31.
	if nameSize >= 31 || len(body)-9 < int(nameSize)+3 {
		return characterLevelInfo{}, false
	}
	return characterLevelInfo{known: true, uniqueID: selfUnique, level: body[9+int(nameSize)+2]}, true
}

func parseCharacterLevelNotification(body []byte) (byte, bool) {
	// Type 37: level:u8, experience/party/member:u32, four skill-point
	// counters:u16, PvP/fatigue/Seria/weekend/premium:u32. Khazad/8 adds
	// a four-byte trailer and three zero pad bytes to the 41-byte body.
	if len(body) != 41 && len(body) != 48 {
		return 0, false
	}
	if len(body) == 48 && (body[45] != 0 || body[46] != 0 || body[47] != 0) {
		return 0, false
	}
	return body[0], true
}

func (r *RobotVo) handleCharacterLevelUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 || packet.typ != 37 ||
		(r.State != StateLogin && r.State != StateRun) || r.ReturnSelectPending ||
		!r.characterLevel.known || r.characterUniqueID == 0 || r.characterLevel.uniqueID != r.characterUniqueID {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected characterLevelInfo
	for _, candidate := range candidates {
		level, ok := parseCharacterLevelNotification(candidate.body)
		if !ok {
			continue
		}
		info := characterLevelInfo{known: true, uniqueID: r.characterUniqueID, level: level}
		if selected.known && selected != info {
			return
		}
		selected = info
	}
	if selected.known {
		r.applyCharacterLevelUnsafe(selected)
	}
}

func (r *RobotVo) applyCharacterLevelUnsafe(info characterLevelInfo) {
	if info != r.characterLevel {
		r.characterLevel = info
		foundationlog.Robotf("[CHARACTER_LEVEL] uid=%d cid=%d unique=%d level=%d\n", r.UID, r.CID, info.uniqueID, info.level)
	}
}
