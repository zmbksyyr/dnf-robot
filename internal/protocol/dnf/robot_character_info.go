package dnf

import (
	"encoding/binary"

	foundationlog "robot/internal/foundation/log"
)

const characterBaseStatsSize = 82

// USERINFO contains the server's base attributes, before the client's equipment,
// passive and other modifiers. It does not report current or final maximum MP.
type characterBasicInfo struct {
	known      bool
	uniqueID   uint16
	experience uint32
	stats      [characterBaseStatsSize]byte
	stamina    byte // Server stamina percentage; also scales the native primary attributes.
	equipment  characterEquipmentInfo
}

func (info characterBasicInfo) baseMP() uint32 {
	// The native attribute setter divides this unsigned wire value by ten.
	return binary.LittleEndian.Uint32(info.stats[4:8]) / 10
}

// baseMPRegen reports the unmodified recovery attribute, not MP per second.
// The native initializer reads a signed short, divides by ten, and divides by
// five again for each of the eleven supported character jobs. Equipment,
// spirit and runtime modifiers are applied separately after this value.
func (info characterBasicInfo) baseMPRegen(job int) (int32, bool) {
	if !info.known || job < 0 || job > 10 {
		return 0, false
	}
	wire := int32(int16(binary.LittleEndian.Uint16(info.stats[0x40:0x42])))
	return wire / 10 / 5, true
}

func parseSelectedCharacterUnique(body []byte) (uint16, bool) {
	// Successful SELECT_CHARACTER: success:u8, two u32 fields, then the
	// character's world unique:u16 followed by three more u16 fields.
	if len(body) < 17 || body[0] != 1 {
		return 0, false
	}
	uniqueID := binary.LittleEndian.Uint16(body[9:11])
	return uniqueID, uniqueID != 0
}

func parseCharacterBasicInfo(body []byte, selfUnique uint16) (characterBasicInfo, bool) {
	body, err := inflatePartyInfo(body)
	if err != nil {
		return characterBasicInfo{}, false
	}
	return parseInflatedCharacterBasicInfo(body, selfUnique)
}

func parseInflatedCharacterBasicInfo(body []byte, selfUnique uint16) (characterBasicInfo, bool) {
	if selfUnique == 0 {
		return characterBasicInfo{}, false
	}
	// The original server compresses USERINFO. Modes 0/2 describe town users
	// and the character list; mode 3 can describe an inspected character. Only
	// a mode-1 record matching SELECT_CHARACTER initializes our own attributes.
	if len(body) < 13+characterBaseStatsSize+1 || body[0] != 1 || binary.LittleEndian.Uint16(body[1:3]) != 1 {
		return characterBasicInfo{}, false
	}
	uniqueID := binary.LittleEndian.Uint16(body[3:5])
	if uniqueID != selfUnique || binary.LittleEndian.Uint32(body[9:13]) != characterBaseStatsSize+1 {
		return characterBasicInfo{}, false
	}
	info := characterBasicInfo{
		known:      true,
		uniqueID:   uniqueID,
		experience: binary.LittleEndian.Uint32(body[5:9]),
		stamina:    body[13+characterBaseStatsSize],
	}
	copy(info.stats[:], body[13:13+characterBaseStatsSize])
	// A damaged/unsupported equipment section leaves that snapshot unknown;
	// the independently bounded base-stat block is still authoritative.
	info.equipment, _, _ = parseCharacterEquipmentInfo(body[13+characterBaseStatsSize+1:])
	return info, true
}

func (r *RobotVo) handleCharacterInfoUnsafe(packet robotInboundPacket) {
	// Returning to selection also invalidates late selection replies and base
	// snapshots, including their equipment records, before the state changes.
	if r.ReturnSelectPending || r.State != StateLogin && r.State != StateRun {
		return
	}
	if packet.flag == 0 && (packet.typ == 4 || packet.typ == 33) {
		r.handleCharacterStaminaUnsafe(packet)
		return
	}
	if packet.typ == 37 {
		r.handleCharacterLevelUnsafe(packet)
		return
	}
	if packet.typ != 2 && packet.typ != 4 {
		return
	}
	if packet.typ == 4 && (packet.flag != 1 || r.State != StateLogin || !r.SelectCharacSent) {
		return
	}
	if packet.typ == 2 && packet.flag != 0 {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	if packet.typ == 4 {
		var selected uint16
		for _, candidate := range candidates {
			uniqueID, ok := parseSelectedCharacterUnique(candidate.body)
			if !ok {
				continue
			}
			if selected != 0 && selected != uniqueID {
				return
			}
			selected = uniqueID
		}
		if selected != 0 && selected != r.characterUniqueID {
			r.clearCharacterInventoryUnsafe()
			r.characterUniqueID = selected
			r.characterBaseInfo = characterBasicInfo{}
			r.characterResources = characterResourcePool{}
			r.characterLevel = characterLevelInfo{}
		}
		return
	}
	var selected characterBasicInfo
	var selectedLevel characterLevelInfo
	for _, candidate := range candidates {
		body, err := inflatePartyInfo(candidate.body)
		if err != nil {
			continue
		}
		if info, ok := parseInflatedCharacterBasicInfo(body, r.characterUniqueID); ok {
			if selected.known && selected != info {
				return
			}
			selected = info
		}
		if level, ok := parseInflatedCharacterLevelInfo(body, r.characterUniqueID); ok {
			if selectedLevel.known && selectedLevel != level {
				return
			}
			selectedLevel = level
		}
	}
	if selectedLevel.known {
		r.applyCharacterLevelUnsafe(selectedLevel)
	}
	if selected.known && selected != r.characterBaseInfo {
		r.characterBaseInfo = selected
		var regen int32
		var regenKnown bool
		if r.partySkillLoaded {
			regen, regenKnown = selected.baseMPRegen(r.partySkillJob)
		}
		foundationlog.Robotf("[CHARACTER_BASE_INFO] uid=%d cid=%d unique=%d base_mp=%d equipment_known=%t equipped=%d base_mp_regen=%d base_mp_regen_known=%t stamina=%d\n", r.UID, r.CID, selected.uniqueID, selected.baseMP(), selected.equipment.known, selected.equipment.count(), regen, regenKnown, selected.stamina)
	}
}
