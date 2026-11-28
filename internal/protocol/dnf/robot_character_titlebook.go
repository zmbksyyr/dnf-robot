package dnf

import (
	"bytes"
	"encoding/binary"
	"maps"

	foundationlog "robot/internal/foundation/log"
)

const (
	characterTitlebookList       = 357
	characterAchievementList     = 358
	characterAchievementComplete = 359
	characterTitlebookPut        = 412
	characterTitlebookGet        = 413
	characterAchievementTrigger  = 417
	characterTitlebookRecordSize = 21
	characterAchievementLimit    = 512
)

// CTitleBook stores 61-byte Inven_Item records, but _putItemData sends only
// these 21 bytes. Category order is general, specific, despair, event.
var characterTitlebookCapacities = [4]int{70, 200, 120, 60}

type characterTitlebookCategory struct {
	known bool
	slots map[uint16][characterTitlebookRecordSize]byte
}

// These are observed inputs, not applied collection rewards. The achievement
// words are remaining trigger counts: native stAchievement::isComplete tests
// all three for zero. Slot occupancy is a separate input.
type characterTitlebookInfo struct {
	categories        [4]characterTitlebookCategory
	achievementsKnown bool
	achievements      map[uint32][3]uint16
}

func (info *characterTitlebookInfo) summary() (categories, titles, achievements, completed int) {
	for _, category := range info.categories {
		if category.known {
			categories++
			titles += len(category.slots)
		}
	}
	achievements = len(info.achievements)
	for _, remaining := range info.achievements {
		if remaining == [3]uint16{} {
			completed++
		}
	}
	return
}

// The native packet adds a four-byte finalize trailer and block padding after
// the fields. Plain transports can supply just the fields. Do not mistake
// trailer or padding bytes for another title or achievement.
func characterTitlebookBodyFits(body []byte, used int) bool {
	if used < 0 || used > len(body) {
		return false
	}
	if used == len(body) {
		return true
	}
	if len(body)-used < 4 || len(body)-used > 19 {
		return false
	}
	for _, padding := range body[used+4:] {
		if padding != 0 {
			return false
		}
	}
	return true
}

// parseCharacterTitlebookCategory accepts only our own full category list.
// sendList uses info type=0 and owner unique=0; inspected users have a nonzero
// owner unique and may include their equipped title as an extra record.
func parseCharacterTitlebookCategory(body []byte) (int, characterTitlebookCategory, int, bool) {
	if len(body) < 11 || body[0] != 0 || binary.LittleEndian.Uint16(body[1:3]) != 0 {
		return 0, characterTitlebookCategory{}, 0, false
	}
	category := int(binary.LittleEndian.Uint32(body[3:7]))
	if category < 0 || category >= len(characterTitlebookCapacities) {
		return 0, characterTitlebookCategory{}, 0, false
	}
	count := int(binary.LittleEndian.Uint32(body[7:11]))
	if count < 0 || count > characterTitlebookCapacities[category] {
		return 0, characterTitlebookCategory{}, 0, false
	}
	used := 11 + count*characterTitlebookRecordSize
	if !characterTitlebookBodyFits(body, used) {
		return 0, characterTitlebookCategory{}, 0, false
	}
	info := characterTitlebookCategory{known: true}
	if count != 0 {
		info.slots = make(map[uint16][characterTitlebookRecordSize]byte, count)
	}
	for pos := 11; pos < used; pos += characterTitlebookRecordSize {
		slot := binary.LittleEndian.Uint16(body[pos:])
		if int(slot) >= characterTitlebookCapacities[category] || binary.LittleEndian.Uint32(body[pos+2:]) == 0 {
			return 0, characterTitlebookCategory{}, 0, false
		}
		if _, duplicate := info.slots[slot]; duplicate {
			return 0, characterTitlebookCategory{}, 0, false
		}
		var record [characterTitlebookRecordSize]byte
		copy(record[:], body[pos:pos+characterTitlebookRecordSize])
		info.slots[slot] = record
	}
	return category, info, used, true
}

func parseCharacterAchievements(body []byte) (map[uint32][3]uint16, int, bool) {
	if len(body) < 4 {
		return nil, 0, false
	}
	count := int(binary.LittleEndian.Uint32(body))
	if count < 0 || count > characterAchievementLimit {
		return nil, 0, false
	}
	used := 4 + count*10
	if !characterTitlebookBodyFits(body, used) {
		return nil, 0, false
	}
	var entries map[uint32][3]uint16
	if count != 0 {
		entries = make(map[uint32][3]uint16, count)
	}
	for pos := 4; pos < used; pos += 10 {
		id := binary.LittleEndian.Uint32(body[pos:])
		if _, duplicate := entries[id]; duplicate {
			return nil, 0, false
		}
		entries[id] = characterAchievementRemaining(body[pos+4:])
	}
	return entries, used, true
}

func characterAchievementRemaining(body []byte) [3]uint16 {
	return [3]uint16{binary.LittleEndian.Uint16(body), binary.LittleEndian.Uint16(body[2:]), binary.LittleEndian.Uint16(body[4:])}
}

func characterTitlebookMoveCategoryOffset(typ uint16) int {
	if typ == characterTitlebookGet {
		return 1 // category, title slot, destination space, destination slot
	}
	return 9 // source space, source slot, category, title slot
}

type characterTitlebookUpdate struct {
	payload      []byte
	category     int
	snapshot     characterTitlebookCategory
	achievements map[uint32][3]uint16
}

// parseCharacterTitlebookUpdate validates a candidate before any mutation.
// Completion and successful moves lack a full item record, so they invalidate
// that category until another list arrives; they cannot create empty slots.
func parseCharacterTitlebookUpdate(typ uint16, body []byte) (characterTitlebookUpdate, bool) {
	var update characterTitlebookUpdate
	var used int
	var ok bool
	switch typ {
	case characterTitlebookList:
		update.category, update.snapshot, used, ok = parseCharacterTitlebookCategory(body)
	case characterAchievementList:
		update.achievements, used, ok = parseCharacterAchievements(body)
	case characterAchievementComplete:
		if len(body) < 18 {
			return update, false
		}
		category, slot := binary.LittleEndian.Uint32(body[4:]), binary.LittleEndian.Uint32(body[8:])
		used, ok = 18, category < 4 && slot < uint32(characterTitlebookCapacities[category]) && characterTitlebookBodyFits(body, 18)
	case characterTitlebookPut, characterTitlebookGet:
		if len(body) < 17 || body[0] != 1 {
			return update, false
		}
		offset := characterTitlebookMoveCategoryOffset(typ)
		category, slot := binary.LittleEndian.Uint32(body[offset:]), binary.LittleEndian.Uint32(body[offset+4:])
		used, ok = 17, category < 4 && slot < uint32(characterTitlebookCapacities[category]) && characterTitlebookBodyFits(body, 17)
	case characterAchievementTrigger:
		used, ok = 11, len(body) >= 11 && body[0] == 1 && characterTitlebookBodyFits(body, 11)
	}
	if ok {
		update.payload = body[:used]
	}
	return update, ok
}

func (r *RobotVo) handleCharacterTitlebookUnsafe(packet robotInboundPacket) {
	if r.ReturnSelectPending || r.characterUniqueID == 0 || r.State != StateLogin && r.State != StateRun {
		return
	}
	expectedFlag := byte(0)
	if packet.typ == characterTitlebookPut || packet.typ == characterTitlebookGet || packet.typ == characterAchievementTrigger {
		expectedFlag = 1
	}
	if packet.flag != expectedFlag {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var selected characterTitlebookUpdate
	for _, candidate := range candidates {
		update, ok := parseCharacterTitlebookUpdate(packet.typ, candidate.body)
		if !ok {
			continue
		}
		if selected.payload != nil && !bytes.Equal(selected.payload, update.payload) {
			return
		}
		selected = update
	}
	if selected.payload == nil {
		return
	}
	info := &r.characterResources.titlebook
	changed := false
	switch packet.typ {
	case characterTitlebookList:
		category, snapshot := selected.category, selected.snapshot
		old := info.categories[category]
		changed = !old.known || !maps.Equal(old.slots, snapshot.slots)
		info.categories[category] = snapshot
	case characterAchievementList:
		snapshot := selected.achievements
		changed = !info.achievementsKnown || !maps.Equal(info.achievements, snapshot)
		info.achievements, info.achievementsKnown = snapshot, true
	case characterAchievementComplete, characterAchievementTrigger:
		offset := 0
		if packet.typ == characterAchievementTrigger {
			offset = 1
		}
		id := binary.LittleEndian.Uint32(selected.payload[offset:])
		remaining := [3]uint16{}
		if packet.typ == characterAchievementTrigger {
			remaining = characterAchievementRemaining(selected.payload[5:])
		} else {
			category := binary.LittleEndian.Uint32(selected.payload[4:])
			changed = info.categories[category].known
			info.categories[category] = characterTitlebookCategory{}
		}
		old, known := info.achievements[id]
		if !known && len(info.achievements) >= characterAchievementLimit {
			// An incremental stream must not grow beyond the native full-list
			// bound. Discard the snapshot instead of keeping partial certainty.
			info.achievements, info.achievementsKnown = nil, false
		} else {
			if info.achievements == nil {
				info.achievements = make(map[uint32][3]uint16)
			}
			info.achievements[id] = remaining
		}
		changed = changed || !known || old != remaining
	case characterTitlebookPut, characterTitlebookGet:
		category := binary.LittleEndian.Uint32(selected.payload[characterTitlebookMoveCategoryOffset(packet.typ):])
		changed = info.categories[category].known
		info.categories[category] = characterTitlebookCategory{}
	}
	if changed {
		categories, titles, achievements, completed := info.summary()
		foundationlog.Robotf("[CHARACTER_TITLEBOOK] uid=%d cid=%d source_type=%d categories_known=%d titles=%d achievements_known=%t achievements=%d completed=%d collection_rewards_applied=false\n", r.UID, r.CID, packet.typ, categories, titles, info.achievementsKnown, achievements, completed)
	}
}
