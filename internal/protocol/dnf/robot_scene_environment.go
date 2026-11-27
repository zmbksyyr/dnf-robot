package dnf

import "encoding/binary"

// Scene+0xf90 is the channel category from the successful login reply. It
// belongs to the account connection and survives a return to character select.
// It is independent of the current scene mode and dungeon/room identities.
type sceneCategoryInfo struct {
	known    bool
	category byte
}

func parseLoginSceneCategory(body []byte) (sceneCategoryInfo, bool) {
	// Success:u8, account-name length:u32, account-name bytes, three flag
	// bytes, age:u8, PC-room:u8, channel-category:u8. The remaining login
	// fields have separate consumers; this reads only the category prefix.
	if len(body) < 11 || body[0] == 0 {
		return sceneCategoryInfo{}, false
	}
	nameSize := binary.LittleEndian.Uint32(body[1:5])
	// Native 0x714170 uses a 21-byte buffer through 0x1134650, which only
	// consumes string data for lengths below 21. Do not guess another offset.
	if nameSize >= 21 || len(body)-11 < int(nameSize) {
		return sceneCategoryInfo{}, false
	}
	category := body[10+int(nameSize)]
	// Native 0x71459d normalizes category 5 before calling 0x75ab00.
	if category == 5 {
		category = 4
	}
	return sceneCategoryInfo{known: true, category: category}, true
}

func (r *RobotVo) selectSceneCategoryUnsafe(packet robotInboundPacket) sceneCategoryInfo {
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected sceneCategoryInfo
	for _, candidate := range candidates {
		category, valid := parseLoginSceneCategory(candidate.body)
		if !valid {
			continue
		}
		if selected.known && selected != category {
			return sceneCategoryInfo{}
		}
		selected = category
	}
	return selected
}
