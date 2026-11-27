package dnf

import (
	"encoding/binary"
	"math/bits"
)

const partyPassiveObjectCommand = 0x0015

// Object creation has an additional rolling word mask inside the ordinary
// byte-XOR envelope. The final key/sender/length fields are outside that mask;
// a final partial word is unmasked too. Coordinates and object-specific data
// remain opaque here: their layout varies between observed object forms.
func decodePartyPassiveObjectBody(body []byte) (data []byte, sender uint16, ok bool) {
	if len(body) < 7+24+10 || body[0] != 2 || binary.LittleEndian.Uint16(body[1:3]) != partyPassiveObjectCommand {
		return nil, 0, false
	}
	payload := make([]byte, len(body)-7)
	for i, value := range body[7:] {
		payload[i] = value ^ 0x7e
	}
	checksum := partyPayloadChecksum(payload)
	if body[3] != checksum[0] {
		return nil, 0, false
	}
	end := len(payload) - 10
	key := binary.LittleEndian.Uint32(payload[end:])
	sender = binary.LittleEndian.Uint16(payload[end+4:])
	if sender == 0 || binary.LittleEndian.Uint32(payload[end+6:]) != uint32(len(payload)+3) {
		return nil, 0, false
	}
	data = payload[:end]
	for i := 0; i+4 <= len(data); i += 4 {
		value := binary.LittleEndian.Uint32(data[i:]) ^ bits.RotateLeft32(key, i/4)
		binary.LittleEndian.PutUint32(data[i:], value)
	}
	return data, sender, true
}

func buildPartyPassiveObjectBody(data []byte, sender uint16, key uint32) ([]byte, bool) {
	// A reliable record also needs its seven-byte header and two-byte length.
	if len(data) < 24 || len(data) > 65535-19 || sender == 0 {
		return nil, false
	}
	payload := make([]byte, len(data)+10)
	copy(payload, data)
	for i := 0; i+4 <= len(data); i += 4 {
		value := binary.LittleEndian.Uint32(data[i:]) ^ bits.RotateLeft32(key, i/4)
		binary.LittleEndian.PutUint32(payload[i:], value)
	}
	binary.LittleEndian.PutUint32(payload[len(data):], key)
	binary.LittleEndian.PutUint16(payload[len(data)+4:], sender)
	binary.LittleEndian.PutUint32(payload[len(data)+6:], uint32(len(payload)+3))
	body := make([]byte, 7+len(payload))
	body[0] = 2
	binary.LittleEndian.PutUint16(body[1:3], partyPassiveObjectCommand)
	checksum := partyPayloadChecksum(payload)
	copy(body[3:7], checksum[:])
	for i, value := range payload {
		body[7+i] = value ^ 0x7e
	}
	return body, true
}
