package dnf

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
)

// killRoomMonstersUnsafe reports every monster in the room packet dead.
//
// The server only counts a kill it hears about on the death dispatcher, and it
// refuses a room move while any monster flagged as must-kill is still alive.
// The room packet carries each monster's instance id, which is what the death
// report needs; the ids are allocated per room load, so they must be read from
// the packet that belongs to the current room.
func (r *RobotVo) killRoomMonstersUnsafe(packet robotInboundPacket) {
	_, _, body, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
	if err != nil || len(body) < 13 {
		return
	}
	count := int(body[12])
	const entrySize = 16
	if count <= 0 || 13+count*entrySize > len(body) {
		return
	}
	r.roomClearIDs = r.roomClearIDs[:0]
	for i := 0; i < count; i++ {
		base := 13 + i*entrySize
		// The instance id sits six bytes into the entry, after the group index
		// and the monster id.
		instID := binary.LittleEndian.Uint16(body[base+6 : base+8])
		r.roomClearIDs = append(r.roomClearIDs, instID)
		mobIndex := binary.LittleEndian.Uint16(body[base+8 : base+10])
		alive := body[base+14]
		if alive != 0 {
			continue
		}
		r.reportMonsterDeathUnsafe(instID)
		if mobIndex != instID {
			// The map may key monsters by the index rather than the instance id;
			// both are harmless when accepted and one of them registers.
			r.reportMonsterDeathUnsafe(mobIndex)
		}
		// The packet may be built before every monster of the room exists, so
		// sweep the neighbouring ids as well: an unknown id is ignored by the
		// server without ending the session.
		if os.Getenv("DNF_LIVE_CLEAR_SWEEP") == "1" {
			for delta := 1; delta <= 16; delta++ {
				r.reportMonsterDeathUnsafe(instID + uint16(delta))
			}
		}
	}
}

// reportMonsterDeathUnsafe sends the death report for one monster instance.
//
// The wire order is the reader's: fixed header, then one block per killed
// monster, then the trailing pairs. One entry comes to exactly sixty-four
// bytes, which is the cipher block size.
func (r *RobotVo) reportMonsterDeathUnsafe(instID uint16) {
	killer := uint16(r.partySelfPeer.uniqueID)
	if raw := strings.TrimSpace(os.Getenv("DNF_LIVE_KILLER")); raw != "" {
		if value, convErr := strconv.Atoi(raw); convErr == nil {
			killer = uint16(value)
		}
	}
	body := make([]byte, 64)
	offset := 0
	putShort := func(value uint16) { binary.LittleEndian.PutUint16(body[offset:], value); offset += 2 }
	putInt := func(value uint32) { binary.LittleEndian.PutUint32(body[offset:], value); offset += 4 }
	putByte := func(value byte) { body[offset] = value; offset++ }

	putShort(instID)     // m_mobSeq
	putShort(killer)     // m_fieldF
	putInt(0)            // m_field11
	putInt(0)            // m_field15
	putInt(0)            // m_killValue2
	putInt(0)            // m_killValue1
	putByte(1)           // m_mobCount
	putShort(0)          // skillList[0]
	putShort(killer)     // attackerList[0]
	putInt(100)          // damageList[0]
	putShort(1)          // hitCountList[0]
	putShort(0)          // m_killPairA24
	putShort(0)          // m_killPairA26
	putByte(0)           // m_fieldA22
	putByte(0)           // m_fieldA23
	putByte(0)           // passive flag
	putByte(0)           // move check flag
	putShort(0)          // unused short
	putByte(0)           // m_fieldA28
	putInt(0)            // m_fieldA2d
	putInt(0)            // m_fieldA31
	for i := 0; i < 7; i++ {
		putShort(0) // m_fieldA29 .. m_fieldA3d
	}
	if packet, err := buildSendPacket(39, uint16(r.PacketID), body, r.Cipher); err == nil {
		r.PacketID++
		r.sendRaw(packet)
		if os.Getenv("DNF_TRACE_RX") == "1" {
			println("[ROOM_CLEAR] reported monster", instID, "dead")
		}
	}
}
// ReportCachedRoomMonsters re-sends the deaths parsed from the last room packet.
//
// The server builds that packet before every monster of the room exists, so a
// report sent immediately can miss; repeating it a moment later hits the ones
// that appeared in the meantime.
func (r *RobotVo) ReportCachedRoomMonsters() int {
	ids := append([]uint16(nil), r.roomClearIDs...)
	for _, id := range ids {
		r.reportMonsterDeathUnsafe(id)
	}
	return len(ids)
}