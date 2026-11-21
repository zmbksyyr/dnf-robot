package dnf

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
)

// roomMonster is one entry of the room information packet.
type roomMonster struct {
	instID   uint16
	mobIndex uint16
	champion bool
}

// parseRoomMonsters reads the monster table out of a room information body.
//
// The server writes a one-byte count and then sixteen bytes per monster: the
// hell party group, the monster id, the instance id, the data index, the level,
// the role, the champion flag and the map position. Only the instance id, the
// data index and the champion flag matter here.
func parseRoomMonsters(body []byte) []roomMonster {
	if len(body) < 13 {
		return nil
	}
	count := int(body[12])
	const entrySize = 16
	if count <= 0 || 13+count*entrySize > len(body) {
		return nil
	}
	monsters := make([]roomMonster, 0, count)
	for i := 0; i < count; i++ {
		base := 13 + i*entrySize
		monsters = append(monsters, roomMonster{
			instID:   binary.LittleEndian.Uint16(body[base+6 : base+8]),
			mobIndex: binary.LittleEndian.Uint16(body[base+8 : base+10]),
			champion: body[base+14] != 0,
		})
	}
	return monsters
}

// killRoomMonstersUnsafe reports every ordinary monster in the room packet dead.
//
// The server only counts a kill it hears about on the death dispatcher, and it
// refuses a room move while any monster in the room is still at full health.
// The ids are allocated per room load, so they have to come from the packet that
// belongs to the current room.
func (r *RobotVo) killRoomMonstersUnsafe(packet robotInboundPacket) {
	_, _, body, err := parseRecvPacket(r.Cipher, packet.data, packet.isAnti)
	if err != nil {
		return
	}
	r.roomClearIDs = r.roomClearIDs[:0]
	for _, monster := range parseRoomMonsters(body) {
		r.roomClearIDs = append(r.roomClearIDs, monster.instID)
		if monster.champion {
			continue
		}
		r.reportMonsterDeathUnsafe(monster.instID)
		if monster.mobIndex != monster.instID {
			// The map may key monsters by the data index rather than the instance
			// id; an unknown key is ignored by the server, so both are harmless.
			r.reportMonsterDeathUnsafe(monster.mobIndex)
		}
		if os.Getenv("DNF_LIVE_CLEAR_SWEEP") == "1" {
			for delta := 1; delta <= 16; delta++ {
				r.reportMonsterDeathUnsafe(monster.instID + uint16(delta))
			}
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

// reportMonsterDeathUnsafe sends the death report for one monster instance.
//
// The wire order is the reader's: fixed header, then one ten-byte block per
// killed monster, then the trailing pairs. One entry comes to exactly sixty-four
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

	putShort(instID) // m_mobSeq
	putShort(killer) // m_fieldF
	putInt(0)        // m_field11
	putInt(0)        // m_field15
	putInt(0)        // m_killValue2
	putInt(0)        // m_killValue1
	putByte(1)       // m_mobCount
	putShort(0)      // skillList[0]
	putShort(killer) // attackerList[0]
	putInt(100)      // damageList[0]
	putShort(1)      // hitCountList[0]
	putShort(0)      // m_killPairA24
	putShort(0)      // m_killPairA26
	putByte(0)       // m_fieldA22
	putByte(0)       // m_fieldA23
	putByte(0)       // passive flag
	putByte(0)       // move check flag
	putShort(0)      // unused short
	putByte(0)       // m_fieldA28
	putInt(0)        // m_fieldA2d
	putInt(0)        // m_fieldA31
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