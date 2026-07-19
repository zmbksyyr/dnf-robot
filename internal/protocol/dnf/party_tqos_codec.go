package dnf

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"math/bits"
	"time"
)

const partyTQOSBodySize = 10

var partyTQOSCRCTable = crc32.MakeTable(0x4db89129)

type partyTQOSCodec struct {
	key    byte
	rotate uint8
}

type partyTQOSPacket struct {
	typ        byte
	sequence   uint32
	senderSlot byte
	flags      byte
	state      byte
	route      byte
	codec      partyTQOSCodec
}

type partyTQOSReliableReply struct {
	packet       []byte
	nextRetry    time.Time
	retries      uint8
	acknowledged bool
	exhausted    bool
}

type partyTQOSReceiveWindow struct {
	latest      uint32
	seen        uint64
	initialized bool
}

func (w *partyTQOSReceiveWindow) accept(sequence uint32) bool {
	if !w.initialized {
		w.latest = sequence
		w.seen = 1
		w.initialized = true
		return true
	}
	delta := int32(sequence - w.latest)
	if delta > 0 {
		if delta >= 64 {
			w.seen = 1
		} else {
			w.seen = (w.seen << uint(delta)) | 1
		}
		w.latest = sequence
		return true
	}
	behind := uint32(w.latest - sequence)
	if behind >= 64 {
		return false
	}
	mask := uint64(1) << behind
	if w.seen&mask != 0 {
		return false
	}
	w.seen |= mask
	return true
}

func splitPartyTransportFrames(payload []byte) ([][]byte, bool) {
	frames := make([][]byte, 0, 2)
	for len(payload) > 0 {
		frameSize := 0
		switch payload[0] {
		case 0x00:
			frameSize = 8
		case 0x01, 0x02:
			if len(payload) < 9 {
				return nil, false
			}
			frameSize = 9 + int(binary.LittleEndian.Uint16(payload[5:7]))
		default:
			return nil, false
		}
		if frameSize <= 0 || frameSize > len(payload) {
			return nil, false
		}
		frames = append(frames, payload[:frameSize])
		payload = payload[frameSize:]
	}
	return frames, len(frames) > 0
}

func buildPartyUnreliablePacket(sequence uint32, senderSlot, flags byte, body []byte) []byte {
	payload := make([]byte, 9+len(body))
	payload[0] = 0x02
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(len(body)))
	payload[7] = senderSlot
	payload[8] = flags
	copy(payload[9:], body)
	return payload
}

func buildPartyReliablePacket(sequence uint32, senderSlot, flags byte, records [][]byte) []byte {
	bodySize := 0
	for _, record := range records {
		bodySize += 2 + len(record)
	}
	payload := make([]byte, 9, 9+bodySize)
	payload[0] = 0x01
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(bodySize))
	payload[7] = senderSlot
	payload[8] = flags
	for _, record := range records {
		sizeOffset := len(payload)
		payload = append(payload, 0, 0)
		binary.LittleEndian.PutUint16(payload[sizeOffset:], uint16(len(record)))
		payload = append(payload, record...)
	}
	return payload
}

func parsePartyTQOSPacket(payload []byte, expectedRoute byte) (partyTQOSPacket, bool) {
	return parsePartyTQOSPacketWithCodec(payload, expectedRoute, nil)
}

func parsePartyTQOSPacketWithCodec(payload []byte, expectedRoute byte, preferred *partyTQOSCodec) (partyTQOSPacket, bool) {
	if len(payload) < 9 || (payload[0] != 0x01 && payload[0] != 0x02) {
		return partyTQOSPacket{}, false
	}
	bodySize := int(binary.LittleEndian.Uint16(payload[5:7]))
	if len(payload) != 9+bodySize {
		return partyTQOSPacket{}, false
	}
	body := payload[9:]
	if payload[0] == 0x01 && len(body) != partyTQOSBodySize {
		if len(body) < 2 {
			return partyTQOSPacket{}, false
		}
		innerSize := int(binary.LittleEndian.Uint16(body[0:2]))
		if innerSize != partyTQOSBodySize || len(body) < 2+innerSize {
			return partyTQOSPacket{}, false
		}
		body = body[2 : 2+innerSize]
	}
	if len(body) != partyTQOSBodySize {
		return partyTQOSPacket{}, false
	}
	if body[0] != 0 || body[1] != 0 || body[2] != 0 {
		return partyTQOSPacket{}, false
	}

	senderSlot := payload[7]
	state, codec, ok := decodePartyTQOSBody(body, senderSlot, expectedRoute, preferred)
	if !ok {
		return partyTQOSPacket{}, false
	}
	return partyTQOSPacket{
		typ:        payload[0],
		sequence:   binary.LittleEndian.Uint32(payload[1:5]),
		senderSlot: senderSlot,
		flags:      payload[8],
		state:      state,
		route:      expectedRoute,
		codec:      codec,
	}, true
}

func decodePartyTQOSBody(body []byte, senderSlot, expectedRoute byte, preferred *partyTQOSCodec) (byte, partyTQOSCodec, bool) {
	if len(body) != partyTQOSBodySize {
		return 0, partyTQOSCodec{}, false
	}
	if preferred != nil {
		if state, ok := decodePartyTQOSBodyWithCodec(body, senderSlot, expectedRoute, *preferred); ok {
			return state, *preferred, true
		}
	}
	for rotate := 0; rotate < 8; rotate++ {
		key := bits.RotateLeft8(body[7], -rotate) ^ senderSlot
		codec := partyTQOSCodec{key: key, rotate: uint8(rotate)}
		if state, ok := decodePartyTQOSBodyWithCodec(body, senderSlot, expectedRoute, codec); ok {
			return state, codec, true
		}
	}
	return 0, partyTQOSCodec{}, false
}

func decodePartyTQOSBodyWithCodec(body []byte, senderSlot, expectedRoute byte, codec partyTQOSCodec) (byte, bool) {
	decodedSlot := bits.RotateLeft8(body[7], -int(codec.rotate)) ^ codec.key
	state := bits.RotateLeft8(body[8], -int(codec.rotate)) ^ codec.key
	route := bits.RotateLeft8(body[9], -int(codec.rotate)) ^ codec.key
	if decodedSlot != senderSlot || state > 3 || route != expectedRoute {
		return 0, false
	}
	checksum := partyTQOSChecksum(senderSlot, state, route)
	return state, bytes.Equal(body[3:7], checksum[:])
}

func buildPartyTQOSPacket(sequence uint32, senderSlot, flags, state, route byte, codec partyTQOSCodec) []byte {
	bodySize := partyTQOSBodySize
	bodyOffset := 9
	typ := byte(0x02)
	if state == 2 {
		typ = 0x01
		bodySize += 2
		bodyOffset += 2
	}
	payload := make([]byte, 9+bodySize)
	payload[0] = typ
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(bodySize))
	payload[7] = senderSlot
	payload[8] = flags
	if typ == 0x01 {
		binary.LittleEndian.PutUint16(payload[9:11], partyTQOSBodySize)
	}
	body := payload[bodyOffset:]
	checksum := partyTQOSChecksum(senderSlot, state, route)
	copy(body[3:7], checksum[:])
	plain := [3]byte{senderSlot, state, route}
	for i, value := range plain {
		body[7+i] = bits.RotateLeft8(value^codec.key, int(codec.rotate))
	}
	return payload
}

func buildPartyTQOSAck(senderSlot byte, sequence uint32) []byte {
	payload := make([]byte, 8)
	payload[1] = senderSlot
	binary.LittleEndian.PutUint32(payload[2:6], sequence+1)
	return payload
}

func nextPartyTQOSState(state byte) (byte, bool) {
	switch state {
	case 3:
		return 0, true
	case 0:
		return 1, true
	case 1:
		return 2, true
	default:
		return 0, false
	}
}

func partyTQOSChecksum(senderSlot, state, route byte) [4]byte {
	return partyPayloadChecksum([]byte{senderSlot, state, route})
}

func partyPayloadChecksum(payload []byte) [4]byte {
	value := crc32.Checksum(payload, partyTQOSCRCTable)
	var checksum [4]byte
	binary.LittleEndian.PutUint32(checksum[:], value)
	checksum[0] ^= checksum[1] ^ checksum[2] ^ checksum[3] ^ 0x18
	return checksum
}
