package dnf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
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
	if binary.LittleEndian.Uint16(request[:2]) == 0 {
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
	return parsePartyIPInfoMembersAt(packet, count, 1, 0, nil)
}

func parsePartyIPInfoMembersAt(packet []byte, count, offset int, slot byte, peers []partyIPPeer) ([]partyIPPeer, bool) {
	if int(slot) == count {
		if len(packet)-offset >= 8 {
			return nil, false
		}
		for _, padding := range packet[offset:] {
			if padding != 0 {
				return nil, false
			}
		}
		return peers, true
	}
	for _, candidate := range partyIPInfoMemberCandidates(packet[offset:], slot) {
		nextPeers := peers
		if candidate.peer.uniqueID != 0 || candidate.peer.accID != 0 {
			peer := candidate.peer
			peer.slotKnown = true
			nextPeers = append(append([]partyIPPeer(nil), peers...), peer)
		}
		if parsed, ok := parsePartyIPInfoMembersAt(packet, count, offset+candidate.size, slot+1, nextPeers); ok {
			return parsed, true
		}
	}
	return nil, false
}

type partyIPInfoMemberCandidate struct {
	peer partyIPPeer
	size int
}

func partyIPInfoMemberCandidates(data []byte, slot byte) []partyIPInfoMemberCandidate {
	candidates := make([]partyIPInfoMemberCandidate, 0, 8)
	for size := 22; size <= 24 && len(data) >= size; size++ {
		if partyIPInfoMemberWithIDValid(data[:size]) {
			candidates = append(candidates, partyIPInfoMemberCandidate{peer: partyIPInfoMemberWithID(data[:size], slot), size: size})
		}
	}
	for size := 19; size <= 24 && len(data) >= size; size++ {
		if partyIPInfoMemberWithoutIDValid(data[:size]) {
			candidates = append(candidates, partyIPInfoMemberCandidate{peer: partyIPInfoMemberWithoutID(data[:size], slot), size: size})
		}
	}
	return candidates
}

func partyIPInfoMemberWithIDValid(entry []byte) bool {
	if len(entry) < 22 {
		return false
	}
	if binary.LittleEndian.Uint16(entry[0:2]) == 0 && partyIPInfoIPZero(entry[2:6]) && partyIPInfoIPZero(entry[6:10]) {
		return true
	}
	return partyIPInfoPrivateIP(entry[2:6]) && partyIPInfoPrivateIP(entry[6:10])
}

func partyIPInfoMemberWithoutIDValid(entry []byte) bool {
	if len(entry) < 19 {
		return false
	}
	return partyIPInfoPrivateIP(entry[0:4]) && partyIPInfoPrivateIP(entry[4:8])
}

func partyIPInfoMemberWithID(entry []byte, slot byte) partyIPPeer {
	return partyIPPeer{
		uniqueID: binary.LittleEndian.Uint16(entry[0:2]),
		accID:    binary.LittleEndian.Uint32(entry[12:16]),
		slot:     slot,
		innerIP:  net.IPv4(entry[2], entry[3], entry[4], entry[5]),
		outerIP:  net.IPv4(entry[6], entry[7], entry[8], entry[9]),
		port:     binary.BigEndian.Uint16(entry[10:12]),
		natType:  entry[16],
		mtu:      binary.LittleEndian.Uint32(entry[17:21]),
	}
}

func partyIPInfoMemberWithoutID(entry []byte, slot byte) partyIPPeer {
	return partyIPPeer{
		accID:   binary.LittleEndian.Uint32(entry[10:14]),
		slot:    slot,
		innerIP: net.IPv4(entry[0], entry[1], entry[2], entry[3]),
		outerIP: net.IPv4(entry[4], entry[5], entry[6], entry[7]),
		port:    binary.BigEndian.Uint16(entry[8:10]),
		natType: entry[14],
		mtu:     binary.LittleEndian.Uint32(entry[15:19]),
	}
}

func partyIPInfoPrivateIP(ip []byte) bool {
	if len(ip) < 4 {
		return false
	}
	return ip[0] == 10 || (ip[0] == 192 && ip[1] == 168) || (ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31)
}

func partyIPInfoIPZero(ip []byte) bool {
	return len(ip) >= 4 && ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] == 0
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

type recvBodySource uint8

const (
	recvBodySourceUnknown recvBodySource = iota
	recvBodySourceDecrypted
	recvBodySourcePlain
)

func (s recvBodySource) String() string {
	switch s {
	case recvBodySourceDecrypted:
		return "decrypted"
	case recvBodySourcePlain:
		return "plain"
	default:
		return "unknown"
	}
}

type recvBodyCandidate struct {
	body   []byte
	source recvBodySource
}

func recvBodyCandidates(cipher *crypt.DNFCipher, raw []byte, isAnti bool) ([]recvBodyCandidate, error) {
	_, _, decrypted, decryptErr := parseRecvPacket(cipher, raw, isAnti)
	candidates := make([]recvBodyCandidate, 0, 2)
	if decryptErr == nil {
		candidates = append(candidates, recvBodyCandidate{body: decrypted, source: recvBodySourceDecrypted})
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
			candidates = append(candidates, recvBodyCandidate{body: plain, source: recvBodySourcePlain})
		}
	}
	if len(candidates) == 0 {
		return nil, decryptErr
	}
	return candidates, decryptErr
}

type peerResponseCandidate struct {
	data      []byte
	typ       byte
	source    recvBodySource
	canonical bool
}

func selectPeerResponsePackets(cipher *crypt.DNFCipher, raw []byte, isAnti bool, preferred recvBodySource, knownUniqueID func(uint16) bool) (peerResponseCandidate, *peerResponseCandidate, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	valid := make([]peerResponseCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		response, typ, ok := buildPeerResponse(candidate.body)
		if ok {
			valid = append(valid, peerResponseCandidate{
				data:      response,
				typ:       typ,
				source:    candidate.source,
				canonical: canonicalPeerRequestBody(candidate.body),
			})
		}
	}
	if len(valid) == 1 {
		return valid[0], nil, nil
	}
	if len(valid) > 1 && knownUniqueID != nil {
		known := make([]peerResponseCandidate, 0, len(valid))
		for _, candidate := range valid {
			if knownUniqueID(binary.LittleEndian.Uint16(candidate.data[:2])) {
				known = append(known, candidate)
			}
		}
		if len(known) == 1 {
			return known[0], alternatePartyResponse(valid, known[0]), nil
		}
	}
	if len(valid) > 1 {
		canonical := make([]peerResponseCandidate, 0, len(valid))
		for _, candidate := range valid {
			if candidate.canonical {
				canonical = append(canonical, candidate)
			}
		}
		if len(canonical) == 1 {
			return canonical[0], alternatePartyResponse(valid, canonical[0]), nil
		}
	}
	if len(valid) > 1 && !isAnti && len(raw) >= 15 {
		shapeSource := recvBodySourceDecrypted
		if len(raw)-15 <= 8 {
			shapeSource = recvBodySourcePlain
		}
		for _, candidate := range valid {
			if candidate.source == shapeSource {
				return candidate, alternatePartyResponse(valid, candidate), nil
			}
		}
	}
	if len(valid) > 1 && preferred != recvBodySourceUnknown {
		for _, candidate := range valid {
			if candidate.source == preferred {
				return candidate, alternatePartyResponse(valid, candidate), nil
			}
		}
	}
	if len(valid) > 1 {
		return peerResponseCandidate{}, nil, fmt.Errorf("peer request body is ambiguous between plain and decrypted forms")
	}
	if decryptErr != nil {
		return peerResponseCandidate{}, nil, decryptErr
	}
	return peerResponseCandidate{}, nil, fmt.Errorf("peer request has no valid body")
}

func canonicalPeerRequestBody(body []byte) bool {
	if len(body) < 7 {
		return false
	}
	if len(body) == 7 {
		return true
	}
	for _, padding := range body[7:] {
		if padding != 0 {
			return false
		}
	}
	return true
}

func selectPeerResponsePacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool, preferred recvBodySource, knownUniqueID func(uint16) bool) ([]byte, byte, recvBodySource, error) {
	selected, _, err := selectPeerResponsePackets(cipher, raw, isAnti, preferred, knownUniqueID)
	return selected.data, selected.typ, selected.source, err
}

func alternatePartyResponse(candidates []peerResponseCandidate, selected peerResponseCandidate) *peerResponseCandidate {
	if selected.typ != peerRequestParty && (selected.typ != peerRequestTrade || !selected.canonical) {
		return nil
	}
	for _, candidate := range candidates {
		if candidate.source == selected.source || candidate.typ != peerRequestParty || bytes.Equal(candidate.data, selected.data) {
			continue
		}
		if selected.typ == peerRequestTrade && !candidate.canonical {
			continue
		}
		alternate := candidate
		return &alternate
	}
	return nil
}

func selectPartyIPInfoPacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool, selfAccID uint32) (partyIPPeer, []partyIPPeer, recvBodySource, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	for _, candidate := range candidates {
		self, peers, ok := parsePartyIPInfoSnapshot(candidate.body, selfAccID)
		if !ok {
			continue
		}
		if selfAccID != 0 && self.accID != selfAccID {
			continue
		}
		return self, peers, candidate.source, nil
	}
	if decryptErr != nil {
		return partyIPPeer{}, nil, recvBodySourceUnknown, decryptErr
	}
	return partyIPPeer{}, nil, recvBodySourceUnknown, fmt.Errorf("party IP info has no valid body")
}

func partyIPInfoDebugSummary(cipher *crypt.DNFCipher, raw []byte, isAnti bool) string {
	candidates, err := recvBodyCandidates(cipher, raw, isAnti)
	if len(candidates) == 0 {
		if err != nil {
			return "none err=" + err.Error()
		}
		return "none"
	}
	text := ""
	for _, candidate := range candidates {
		if text != "" {
			text += ";"
		}
		body := candidate.body
		head := body
		if len(head) > 96 {
			head = head[:96]
		}
		count := 0
		if len(body) > 0 {
			count = int(body[0])
		}
		text += fmt.Sprintf("%s len=%d count=%d hex=%s", candidate.source, len(body), count, hex.EncodeToString(head))
	}
	return text
}

func partyInfoPacketClearsParty(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (bool, recvBodySource, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	validSource := recvBodySourceUnknown
	for _, candidate := range candidates {
		inflated, err := inflatePartyInfo(candidate.body)
		if err != nil {
			continue
		}
		if validSource == recvBodySourceUnknown {
			validSource = candidate.source
		}
		if partyInfoClearsParty(inflated) {
			return true, candidate.source, nil
		}
	}
	if validSource != recvBodySourceUnknown {
		return false, validSource, nil
	}
	if decryptErr != nil {
		return false, recvBodySourceUnknown, decryptErr
	}
	return false, recvBodySourceUnknown, fmt.Errorf("party info has no valid zlib body")
}

type partyRealtimeIdentity struct {
	uniqueID uint16
	slot     byte
}

func parsePartyRealtimeInfo(data []byte) ([]partyRealtimeIdentity, bool) {
	if len(data) < 1 {
		return nil, false
	}
	count := int(data[0])
	if count > 4 {
		return nil, false
	}
	expected := 1 + count*8
	if len(data) < expected || len(data)-expected > 15 {
		return nil, false
	}
	for _, padding := range data[expected:] {
		if padding != 0 {
			return nil, false
		}
	}
	identities := make([]partyRealtimeIdentity, 0, count)
	seenSlots := [4]bool{}
	for i := 0; i < count; i++ {
		offset := 1 + i*8
		uniqueID := binary.LittleEndian.Uint16(data[offset : offset+2])
		slot := data[offset+4]
		if uniqueID == 0 || slot >= 4 || seenSlots[slot] {
			return nil, false
		}
		seenSlots[slot] = true
		identities = append(identities, partyRealtimeIdentity{uniqueID: uniqueID, slot: slot})
	}
	return identities, true
}

func selectPartyRealtimeInfoPacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool) ([]partyRealtimeIdentity, recvBodySource, error) {
	candidates, decryptErr := recvBodyCandidates(cipher, raw, isAnti)
	type validCandidate struct {
		identities []partyRealtimeIdentity
		source     recvBodySource
	}
	valid := make([]validCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		identities, ok := parsePartyRealtimeInfo(candidate.body)
		if ok {
			valid = append(valid, validCandidate{identities: identities, source: candidate.source})
		}
	}
	for _, candidate := range valid {
		if candidate.source == recvBodySourceDecrypted {
			return candidate.identities, candidate.source, nil
		}
	}
	if len(valid) > 0 {
		return valid[0].identities, valid[0].source, nil
	}
	if decryptErr != nil {
		return nil, recvBodySourceUnknown, decryptErr
	}
	return nil, recvBodySourceUnknown, fmt.Errorf("party realtime info has no valid body")
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
