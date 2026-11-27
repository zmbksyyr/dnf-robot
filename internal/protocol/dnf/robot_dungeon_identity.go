package dnf

import "encoding/binary"

// Type 28 resolves a definition through native 0x439f00, selects its scene
// object through 0x1118750 and writes that definition to the actual area's
// metadata at 0x733a18. This identity comes from that notification, rather than
// the selection UI or a type 29 room-map index.
type dungeonDefinitionIdentity struct {
	known bool
	id    int32
}

func parseDungeonDefinitionIdentity(body []byte) (dungeonDefinitionIdentity, bool) {
	// dungeon:u16, difficulty:u8, six field bytes, ridable count:u8,
	// count pairs of byte coordinates, floor:u16, random buff:u8.
	if len(body) < 13 {
		return dungeonDefinitionIdentity{}, false
	}
	size := 13 + 2*int(body[9])
	if len(body) != size {
		// The ordinary type 28 Shift/4 envelope appends a four-byte trailer
		// and rounds the encrypted input size to four bytes. Native reads no
		// alignment bytes, and the server does not validate or zero them here.
		// An anti or plain notification can omit this envelope.
		if len(body) != alignTo(size+4, 4) {
			return dungeonDefinitionIdentity{}, false
		}
	}
	return dungeonDefinitionIdentity{known: true, id: int32(binary.LittleEndian.Uint16(body))}, true
}

func (r *RobotVo) selectDungeonDefinitionIdentityUnsafe(packet robotInboundPacket) dungeonDefinitionIdentity {
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected dungeonDefinitionIdentity
	for _, candidate := range candidates {
		identity, valid := parseDungeonDefinitionIdentity(candidate.body)
		if !valid {
			continue
		}
		if selected.known && selected != identity {
			return dungeonDefinitionIdentity{}
		}
		selected = identity
	}
	return selected
}
