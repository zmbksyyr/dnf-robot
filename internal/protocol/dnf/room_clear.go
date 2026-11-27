package dnf

import (
	"encoding/binary"
)

// Like party and town notifications, START_MAP can arrive with a plain body
// outside the anti envelope. Validate both existing receive forms before
// acknowledging the room; plaintext need not have an encrypted block length.
func (r *RobotVo) selectRoomLoadingBodyUnsafe(packet robotInboundPacket) ([]byte, bool) {
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected *recvBodyCandidate
	for i := range candidates {
		candidate := &candidates[i]
		if !validRoomLoadingInfo(candidate.body) {
			continue
		}
		if selected == nil || candidate.source == r.partyRecvSource {
			selected = candidate
		}
	}
	if selected == nil {
		return nil, false
	}
	r.rememberPartyRecvSourceUnsafe(selected.source)
	return selected.body, true
}

// roomMonster is one entry of the room information packet.
type roomMonster struct {
	instID   uint16
	mobIndex uint16
	champion bool
}

// A cleared START_MAP omits the new-room table. The observed empty variant
// has the shared nine-byte room prefix, zero monster/ridable counts and the
// 0xff enter-each-map sentinel. Ordinary encryption pads those twelve bytes to
// sixteen with arbitrary bytes; byte 12 is not a monster count in this form.
func validRoomLoadingInfo(body []byte) bool {
	if hasClearedRoomPrefix(body) {
		return len(body) == 12 || len(body) == 16
	}
	return validRoomMonsterTable(body)
}

func hasClearedRoomPrefix(body []byte) bool {
	return len(body) >= 12 && len(body) <= 16 && body[9] == 0 && body[10] == 0 && body[11] == 0xff
}

// START_MAP writes a random seed at offset 3. Only a new-room body includes
// the PVF map index: a short at offset 10, immediately before the monster count.
// The cleared-room variant omits it, so it must not select another map's objects.
func roomMapID(body []byte) (uint32, bool) {
	if hasClearedRoomPrefix(body) || !validRoomMonsterTable(body) {
		return 0, false
	}
	return uint32(binary.LittleEndian.Uint16(body[10:12])), true
}

// parseRoomMonsters reads the monster table out of a room information body.
//
// The server writes a one-byte count and then sixteen bytes per monster: the
// hell party group, the monster id, the instance id, the data index, the level,
// the role, the champion flag and the map position. Only the instance id, the
// data index and the champion flag matter here.
func validRoomMonsterTable(body []byte) bool {
	return len(body) >= 13 && 13+int(body[12])*16 <= len(body)
}

func parseRoomMonsters(body []byte) []roomMonster {
	if !validRoomMonsterTable(body) {
		return nil
	}
	count := int(body[12])
	const entrySize = 16
	if count == 0 {
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
