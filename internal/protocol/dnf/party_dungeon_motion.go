package dnf

import (
	"encoding/binary"
	"math"
)

// A motion record starts with an entity count. Each entity has two references,
// two room-grid coordinates, a flags byte and twelve two-bit field modes.
// Explicit four-byte fields follow in descending groups of three, not in bit
// order. Only the position group is interpreted as floats here; other groups
// also contain integer properties.
type partyDungeonMotion struct {
	entity, controller [4]byte
	flags              [3]byte
	modes              uint32
	values             [12]uint32
}

func decodePartyDungeonMotion(body []byte) ([]partyDungeonMotion, bool) {
	if len(body) < 8 || body[0] != 1 || binary.LittleEndian.Uint16(body[1:3]) != partyDungeonStateCommand {
		return nil, false
	}
	payload := make([]byte, len(body)-7)
	for i, value := range body[7:] {
		payload[i] = value ^ 0x7e
	}
	checksum := partyPayloadChecksum(payload)
	if string(body[3:7]) != string(checksum[:]) || payload[0] == 0 {
		return nil, false
	}
	count := int(payload[0])
	payload = payload[1:]
	if count > len(payload)/14 {
		return nil, false
	}
	entries := make([]partyDungeonMotion, 0, count)
	for i := 0; i < count; i++ {
		if len(payload) < 14 {
			return nil, false
		}
		var entry partyDungeonMotion
		copy(entry.entity[:], payload[:4])
		copy(entry.controller[:], payload[4:8])
		copy(entry.flags[:], payload[8:11])
		entry.modes = uint32(payload[11]) | uint32(payload[12])<<8 | uint32(payload[13])<<16
		payload = payload[14:]
		for group := 9; group >= 0; group -= 3 {
			for field := group; field < group+3; field++ {
				if entry.mode(field) != 0 {
					continue
				}
				if len(payload) < 4 {
					return nil, false
				}
				entry.values[field] = binary.LittleEndian.Uint32(payload)
				payload = payload[4:]
			}
		}
		// Modes 1 and 2 are zero and retain, respectively. A mode-3 position
		// has not been observed; do not invent a value for it.
		for field := 9; field < 12; field++ {
			if entry.mode(field) == 0 {
				value := float64(math.Float32frombits(entry.values[field]))
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, false
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, len(payload) == 0
}

func (entry partyDungeonMotion) mode(field int) uint32 {
	return entry.modes >> (2 * field) & 3
}

func buildPartyDungeonMotion(entries []partyDungeonMotion) ([]byte, bool) {
	if len(entries) == 0 || len(entries) > 255 {
		return nil, false
	}
	// Size the payload exactly and build it inside the frame buffer: this runs for
	// every outgoing party frame and used to cost seven allocations.
	size := 1
	for _, entry := range entries {
		size += 14
		for field := 0; field < 12; field++ {
			if entry.mode(field) == 0 {
				size += 4
			}
		}
	}
	body := make([]byte, 7, 7+size)
	body[0], body[1] = 1, 4
	body = append(body, byte(len(entries)))
	for _, entry := range entries {
		body = append(body, entry.entity[:]...)
		body = append(body, entry.controller[:]...)
		body = append(body, entry.flags[:]...)
		body = append(body, byte(entry.modes), byte(entry.modes>>8), byte(entry.modes>>16))
		for group := 9; group >= 0; group -= 3 {
			for field := group; field < group+3; field++ {
				if entry.mode(field) == 0 {
					body = binary.LittleEndian.AppendUint32(body, entry.values[field])
				}
			}
		}
	}
	checksum := partyPayloadChecksum(body[7:])
	copy(body[3:7], checksum[:])
	for i := 7; i < len(body); i++ {
		body[i] ^= 0x7e
	}
	return body, true
}

type partyDungeonPosition struct {
	x, y, z float32
	known   byte
}

func (position partyDungeonPosition) apply(entry partyDungeonMotion) partyDungeonPosition {
	values := [...]*float32{&position.x, &position.y, &position.z}
	for i, value := range values {
		switch entry.mode(9 + i) {
		case 0:
			*value = math.Float32frombits(entry.values[9+i])
			position.known |= 1 << i
		case 1:
			*value = 0
			position.known |= 1 << i
		case 3:
			position.known &^= 1 << i
		}
	}
	return position
}
