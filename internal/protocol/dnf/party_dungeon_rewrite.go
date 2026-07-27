package dnf

import (
	"bytes"
	"encoding/binary"
)

const (
	partyDungeonStateCommand          = 0x0004
	partyDungeonEnvelopeCommand       = 0x0051
	partyDungeonEnvelopeMinBodySize   = 26
	partyDungeonEnvelopePayloadOffset = 22
)

var partyDungeonEnvelopeChecksumOffsets = [...]int{10, 18}

func rewritePartyDungeonBody(body []byte, sourceUniqueID, targetUniqueID uint16) ([]byte, uint16, bool) {
	if len(body) < 7 || body[0] != 1 || sourceUniqueID == 0 || targetUniqueID == 0 || sourceUniqueID == targetUniqueID {
		return nil, 0, false
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	followBody := append([]byte(nil), body...)
	var checksum [4]byte
	switch command {
	case partyDungeonEnvelopeCommand:
		if len(body) < partyDungeonEnvelopeMinBodySize {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(body[7:])
		if !bytes.Equal(body[3:7], checksum[:]) {
			return nil, 0, false
		}
		innerChecksum := partyPayloadChecksum(body[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			if !bytes.Equal(body[offset:offset+len(innerChecksum)], innerChecksum[:]) {
				return nil, 0, false
			}
		}
		if rewritePartyDungeonEnvelopeIdentity(followBody[partyDungeonEnvelopePayloadOffset:], sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		innerChecksum = partyPayloadChecksum(followBody[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			copy(followBody[offset:offset+len(innerChecksum)], innerChecksum[:])
		}
	case partyDungeonStateCommand:
		plain := followBody[7:]
		for index := range plain {
			plain[index] ^= 0x7e
		}
		checksum = partyPayloadChecksum(plain)
		if !bytes.Equal(body[3:7], checksum[:]) || rewritePartyDungeonStateIdentity(plain, sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(plain)
		copy(followBody[3:7], checksum[:])
		for index := range plain {
			plain[index] ^= 0x7e
		}
	default:
		return nil, 0, false
	}
	if command == partyDungeonEnvelopeCommand {
		checksum = partyPayloadChecksum(followBody[7:])
		copy(followBody[3:7], checksum[:])
	}
	return followBody, command, true
}

func rewritePartyDungeonRecords(body []byte, sourceUniqueID, targetUniqueID uint16) [][]byte {
	records := make([][]byte, 0, 2)
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return nil
		}
		if record, _, ok := rewritePartyDungeonBody(body[:size], sourceUniqueID, targetUniqueID); ok {
			records = append(records, record)
		}
		body = body[size:]
	}
	if len(body) != 0 {
		return nil
	}
	return records
}

func rewritePartyDungeonEnvelopeIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	if len(payload) < 6 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{2, 4} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
}

func rewritePartyDungeonStateIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	const singleStateHeaderSize = 15
	if len(payload) < singleStateHeaderSize || payload[0] != 1 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{3, 7} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
}

func buildFinishLoadingPayload(inventoryChecksum, skillChecksum uint32) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[:4], inventoryChecksum)
	binary.LittleEndian.PutUint32(body[4:], skillChecksum)
	return body
}
