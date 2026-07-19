package dnf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"robot/internal/protocol/dnf/crypt"
)

func buildSendPacket(sendType, sendIndex uint16, rawData []byte, cipher *crypt.DNFCipher) ([]byte, error) {
	outSize := 13 + len(rawData)
	outBuf := make([]byte, outSize)
	tmp := make([]byte, outSize)

	binary.LittleEndian.PutUint16(tmp[11:13], sendIndex)
	copy(tmp[13:], rawData)

	crc := cipher.CRC32(0, tmp[11:])
	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, crc)
	hashBytes[0] ^= hashBytes[2] ^ hashBytes[1] ^ hashBytes[3] ^ 0x18
	hashVal := binary.LittleEndian.Uint32(hashBytes)

	outBuf[0] = 1
	binary.LittleEndian.PutUint16(outBuf[1:3], sendType)
	binary.LittleEndian.PutUint32(outBuf[3:7], uint32(outSize))
	binary.LittleEndian.PutUint32(outBuf[7:11], hashVal)
	binary.LittleEndian.PutUint16(outBuf[11:13], sendIndex)

	copy(tmp[:13], outBuf[:13])

	if len(rawData) > 0 {
		encrypted, err := cipher.Encrypt(sendType, rawData)
		if err != nil {
			return nil, err
		}
		copy(outBuf[13:], encrypted)
	}

	return outBuf, nil
}

const (
	peerRequestParty byte = iota
	peerRequestTrade
	gameEtcOptionSize = 72
	partyRejectOption = 6
)

func buildPeerResponse(request []byte) ([]byte, byte, bool) {
	if len(request) < 7 {
		return nil, 0, false
	}
	typ := request[2]
	if typ != peerRequestParty && typ != peerRequestTrade {
		return nil, typ, false
	}
	response := make([]byte, 8)
	copy(response, request[:7])
	return response, typ, true
}

func partyAcceptGameOptions(packet []byte) ([]byte, bool) {
	if len(packet) < 4 {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint32(packet[0:4]))
	if size < gameEtcOptionSize || size > len(packet)-4 {
		return nil, false
	}
	options := make([]byte, gameEtcOptionSize)
	copy(options, packet[4:4+gameEtcOptionSize])
	binary.LittleEndian.PutUint16(options[partyRejectOption*2:], 0)
	return options, true
}

func defaultPartyAcceptGameOptions() []byte {
	options := make([]byte, gameEtcOptionSize)
	for i := 0; i < gameEtcOptionSize/2; i++ {
		binary.LittleEndian.PutUint16(options[i*2:], 0x7fff)
	}
	binary.LittleEndian.PutUint16(options[1*2:], 1)
	binary.LittleEndian.PutUint16(options[partyRejectOption*2:], 0)
	return options
}

func parsePartyIPInfoMembers(packet []byte) ([]partyIPPeer, bool) {
	if len(packet) < 1 {
		return nil, false
	}
	count := int(packet[0])
	if count > 4 {
		return nil, false
	}
	const entrySize = 22
	expectedSize := 1 + count*entrySize
	if len(packet) < expectedSize || len(packet)-expectedSize >= 8 {
		return nil, false
	}
	for _, padding := range packet[expectedSize:] {
		if padding != 0 {
			return nil, false
		}
	}
	peers := make([]partyIPPeer, 0, count)
	offset := 1
	for i := 0; i < count; i++ {
		id := binary.LittleEndian.Uint16(packet[offset : offset+2])
		innerIP := net.IPv4(packet[offset+2], packet[offset+3], packet[offset+4], packet[offset+5])
		outerIP := net.IPv4(packet[offset+6], packet[offset+7], packet[offset+8], packet[offset+9])
		port := binary.BigEndian.Uint16(packet[offset+10 : offset+12])
		accID := binary.LittleEndian.Uint32(packet[offset+12 : offset+16])
		natType := packet[offset+16]
		mtu := binary.LittleEndian.Uint32(packet[offset+17 : offset+21])
		if id != 0 {
			peers = append(peers, partyIPPeer{
				uniqueID:  id,
				accID:     accID,
				slot:      byte(i),
				slotKnown: true,
				innerIP:   innerIP,
				outerIP:   outerIP,
				port:      port,
				natType:   natType,
				mtu:       mtu,
			})
		}
		offset += entrySize
	}
	return peers, true
}

func parsePartyIPInfo(packet []byte, selfAccID uint32) []partyIPPeer {
	_, peers, ok := parsePartyIPInfoSnapshot(packet, selfAccID)
	if !ok {
		return nil
	}
	return peers
}

func parsePartySelfIPInfo(packet []byte, selfAccID uint32) partyIPPeer {
	self, _, _ := parsePartyIPInfoSnapshot(packet, selfAccID)
	return self

}

func parsePartyIPInfoSnapshot(packet []byte, selfAccID uint32) (partyIPPeer, []partyIPPeer, bool) {
	members, ok := parsePartyIPInfoMembers(packet)
	if !ok {
		return partyIPPeer{}, nil, false
	}
	peers := make([]partyIPPeer, 0, len(members))
	self := partyIPPeer{}
	for _, peer := range members {
		if selfAccID != 0 && peer.accID == selfAccID {
			self = peer
			continue
		}
		peers = append(peers, peer)
	}
	return self, peers, true
}

func inflatePartyInfo(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func partyInfoClearsParty(data []byte) bool {
	if len(data) < 8 || data[0] != 1 || (data[4] != 2 && data[4] != 3) {
		return false
	}
	return data[5] == 0xff && data[6] == 0xff && data[7] == 0xff
}

type recvBodyCandidate struct {
	body   []byte
	source string
}

func recvBodyCandidates(cipher *crypt.DNFCipher, raw []byte, isAnti bool) ([]recvBodyCandidate, error) {
	_, _, decrypted, decryptErr := parseRecvPacket(cipher, raw, isAnti)
	candidates := make([]recvBodyCandidate, 0, 2)
	if decryptErr == nil {
		candidates = append(candidates, recvBodyCandidate{body: decrypted, source: "decrypted"})
	}
	if plain, ok := plainRecvBody(raw, isAnti); ok {
		duplicate := false
		for _, candidate := range candidates {
			if bytes.Equal(candidate.body, plain) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, recvBodyCandidate{body: plain, source: "plain"})
		}
	}
	if len(candidates) == 0 {
		return nil, decryptErr
	}
	return candidates, decryptErr
}

func selectPeerResponsePacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool) ([]byte, byte, string, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	for _, candidate := range candidates {
		response, typ, ok := buildPeerResponse(candidate.body)
		if ok {
			return response, typ, candidate.source, nil
		}
	}
	if decryptErr != nil {
		return nil, 0, "", decryptErr
	}
	return nil, 0, "", fmt.Errorf("peer request has no valid body")
}

func selectPartyIPInfoPacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool, selfAccID uint32) (partyIPPeer, []partyIPPeer, string, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	var fallbackSelf partyIPPeer
	var fallbackPeers []partyIPPeer
	fallbackSource := ""
	for _, candidate := range candidates {
		self, peers, ok := parsePartyIPInfoSnapshot(candidate.body, selfAccID)
		if !ok {
			continue
		}
		if selfAccID == 0 || self.accID == selfAccID {
			return self, peers, candidate.source, nil
		}
		if fallbackSource == "" {
			fallbackSelf = self
			fallbackPeers = peers
			fallbackSource = candidate.source
		}
	}
	if fallbackSource != "" {
		return fallbackSelf, fallbackPeers, fallbackSource, nil
	}
	if decryptErr != nil {
		return partyIPPeer{}, nil, "", decryptErr
	}
	return partyIPPeer{}, nil, "", fmt.Errorf("party IP info has no valid body")
}

func partyInfoPacketClearsParty(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (bool, string, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	valid := false
	for _, candidate := range candidates {
		inflated, err := inflatePartyInfo(candidate.body)
		if err != nil {
			continue
		}
		valid = true
		if partyInfoClearsParty(inflated) {
			return true, candidate.source, nil
		}
	}
	if valid {
		return false, "", nil
	}
	if decryptErr != nil {
		return false, "", decryptErr
	}
	return false, "", fmt.Errorf("party info has no valid zlib body")
}

func parseRecvPacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (dataType uint16, dataSize int, decrypted []byte, err error) {
	if len(raw) < 15 {
		return 0, 0, nil, fmt.Errorf("packet too short: %d bytes", len(raw))
	}

	dataType = binary.LittleEndian.Uint16(raw[1:3])

	encryptedData := raw[15:]
	if len(encryptedData) == 0 {
		return dataType, 0, nil, nil
	}

	if isAnti {
		dec := make([]byte, len(encryptedData))
		copy(dec, encryptedData)
		return dataType, len(dec), dec, nil
	}

	var decData []byte
	if dataType == 1 {
		decData, err = cipher.DecryptLogin(encryptedData)
	} else {
		decData, err = cipher.Decrypt(dataType, encryptedData)
	}

	if err != nil {
		return dataType, 0, nil, err
	}

	return dataType, len(decData), decData, nil
}

func plainRecvBody(raw []byte, isAnti bool) ([]byte, bool) {
	if isAnti || len(raw) <= 15 {
		return nil, false
	}
	return append([]byte(nil), raw[15:]...), true
}

func alignTo16(size int) int {
	return alignTo(size, 16)
}

func alignTo(size, block int) int {
	return block * ((size / block) + boolToInt(size%block != 0))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
