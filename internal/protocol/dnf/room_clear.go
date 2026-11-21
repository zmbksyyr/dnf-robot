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
	for i := 0; i < count; i++ {
		base := 13 + i*entrySize
		// The instance id sits six bytes into the entry, after the group index
		// and the monster id.
		instID := binary.LittleEndian.Uint16(body[base+6 : base+8])
		alive := body[base+14]
		if instID == 0 || alive != 0 {
			continue
		}
		r.reportMonsterDeathUnsafe(instID)
	}
}

// reportMonsterDeathUnsafe sends the death report for one monster instance.
func (r *RobotVo) reportMonsterDeathUnsafe(instID uint16) {
	body := make([]byte, 0xa40)
	binary.LittleEndian.PutUint16(body[0:2], instID)
	killer := uint16(r.partySelfPeer.uniqueID)
	if raw := strings.TrimSpace(os.Getenv("DNF_LIVE_KILLER")); raw != "" {
		if value, convErr := strconv.Atoi(raw); convErr == nil {
			killer = uint16(value)
		}
	}
	binary.LittleEndian.PutUint16(body[2:4], killer)
	body[12] = byte(roomClearMobCount())
	binary.LittleEndian.PutUint16(body[0x21a:], killer)
	binary.LittleEndian.PutUint32(body[0x41a:], 100)
	binary.LittleEndian.PutUint16(body[0x81a:], 1)
	if packet, err := buildSendPacket(39, uint16(r.PacketID), body, r.Cipher); err == nil {
		r.PacketID++
		r.sendRaw(packet)
		if os.Getenv("DNF_TRACE_RX") == "1" {
			println("[ROOM_CLEAR] reported monster", instID, "dead")
		}
	}
}
// roomClearMobCount keeps the per-mob array length switchable while the exact
// validation is still being pinned down.
func roomClearMobCount() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DNF_LIVE_CLEAR_COUNT"))); err == nil {
		return value
	}
	return 1
}