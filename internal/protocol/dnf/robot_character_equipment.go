package dnf

import (
	"encoding/binary"
)

// USERINFO mode 1 uses the server's 26 equipment slots: avatars 0..9,
// ordinary equipment 10..21, creature 22, and artifacts 23..25. Preserve each
// complete record, including affixes, for attribute calculation. An unknown
// snapshot must never be interpreted as an unequipped character.
type characterEquipmentInfo struct {
	known         bool
	extraSlotStat byte
	slots         [26]string
}

func (info characterEquipmentInfo) itemID(slot int) uint32 {
	if slot < 0 || slot >= len(info.slots) || len(info.slots[slot]) < 5 {
		return 0
	}
	return binary.LittleEndian.Uint32([]byte(info.slots[slot][1:5]))
}

// nativeUpgradeValue reads the instance term used by native 0x7888c0. USERINFO
// 0x7523c0 passes the attribute byte to 0x66a750, which stores only its low five
// bits in state+0xc. The high bits and the record's separate-upgrade byte have
// their own fields. This does not establish that an equipped item is active.
func (info characterEquipmentInfo) nativeUpgradeValue(slot int) (int32, bool) {
	if !info.known || slot < 0 || slot >= len(info.slots) {
		return 0, false
	}
	record := info.slots[slot]
	if len(record) < 27 || int(record[0]) != slot || info.itemID(slot) == 0 {
		return 0, false
	}
	return int32(record[9] & 0x1f), true
}

// nativeEndurance follows USERINFO 0x7523c0's unsigned word read. The state
// constructor and copy preserve it at item+0x1090. In particular, 0xffff is
// 65535, not the signed -1 sentinel used by other native construction paths.
func (info characterEquipmentInfo) nativeEndurance(slot int) (uint16, bool) {
	if !info.known || slot < 0 || slot >= len(info.slots) {
		return 0, false
	}
	record := info.slots[slot]
	if len(record) < 27 || int(record[0]) != slot || info.itemID(slot) == 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint16([]byte(record[10:12])), true
}

func (info characterEquipmentInfo) count() int {
	count := 0
	for _, record := range info.slots {
		if record != "" {
			count++
		}
	}
	return count
}

// parseCharacterEquipmentInfo reads only the equipment section following the
// base-stat block. Remaining USERINFO sections are not equipment records.
func parseCharacterEquipmentInfo(body []byte) (characterEquipmentInfo, int, bool) {
	var info characterEquipmentInfo
	if len(body) < 2 || int(body[1]) > len(info.slots) {
		return info, 0, false
	}
	info.extraSlotStat = body[0]
	pos := 2
	take := func(n int) []byte {
		if n < 0 || n > len(body)-pos {
			return nil
		}
		part := body[pos : pos+n]
		pos += n
		return part
	}
	for i := 0; i < int(body[1]); i++ {
		start := pos
		// slot, item ID, instance/remaining date, attribute, endurance,
		// clear avatar, enchant, amplification type and amplification value.
		base := take(23)
		if base == nil || int(base[0]) >= len(info.slots) || info.slots[base[0]] != "" || binary.LittleEndian.Uint32(base[1:5]) == 0 {
			return characterEquipmentInfo{}, 0, false
		}
		slot := base[0]
		if slot < 10 {
			// Avatar jewel sockets and expansion are length-prefixed binary
			// blocks; the original server emits exactly 30 and 4 bytes.
			for _, size := range []int{30, 4} {
				length := take(4)
				if length == nil || binary.LittleEndian.Uint32(length) != uint32(size) || take(size) == nil {
					return characterEquipmentInfo{}, 0, false
				}
			}
		} else if slot == 22 && take(4) == nil {
			return characterEquipmentInfo{}, 0, false
		}
		// Third Chronicle: at most two options, each int ID + four bytes.
		chronicle := take(1)
		if chronicle == nil || chronicle[0] > 2 || take(int(chronicle[0])*8) == nil {
			return characterEquipmentInfo{}, 0, false
		}
		// Random options have three bytes each. A nonempty list also carries
		// a fortune byte and an optional fourth option (type + four bytes).
		random := take(1)
		if random == nil || random[0] > 3 || take(int(random[0])*3) == nil {
			return characterEquipmentInfo{}, 0, false
		}
		if random[0] != 0 {
			extra := take(2)
			if extra == nil || (extra[1] != 0xff && (extra[1] > 3 || take(4) == nil)) {
				return characterEquipmentInfo{}, 0, false
			}
		}
		// Separate upgrade and trade restriction follow even an empty list.
		if take(2) == nil {
			return characterEquipmentInfo{}, 0, false
		}
		info.slots[slot] = string(body[start:pos])
	}
	info.known = true
	return info, pos, true
}
