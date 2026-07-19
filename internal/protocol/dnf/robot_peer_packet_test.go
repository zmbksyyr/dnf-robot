package dnf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildPeerResponse(t *testing.T) {
	tests := []struct {
		name    string
		request []byte
		typ     byte
		want    []byte
		ok      bool
	}{
		{
			name:    "party",
			request: []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0xaa},
			typ:     peerRequestParty,
			want:    []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0},
			ok:      true,
		},
		{
			name:    "trade",
			request: []byte{0xcd, 0xab, peerRequestTrade, 4, 3, 2, 1},
			typ:     peerRequestTrade,
			want:    []byte{0xcd, 0xab, peerRequestTrade, 4, 3, 2, 1, 0},
			ok:      true,
		},
		{name: "short", request: []byte{1, 2, 3}},
		{name: "unsupported", request: []byte{1, 2, 4, 5, 6, 7, 8}, typ: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, typ, ok := buildPeerResponse(tt.request)
			if ok != tt.ok || typ != tt.typ || !bytes.Equal(got, tt.want) {
				t.Fatalf("buildPeerResponse() = %x, %d, %v; want %x, %d, %v", got, typ, ok, tt.want, tt.typ, tt.ok)
			}
		})
	}
}

func TestPlainRecvBodySupportsPartyNotify(t *testing.T) {
	body := []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0xaa}
	raw := make([]byte, 15+len(body))
	raw[0] = 0
	binary.LittleEndian.PutUint16(raw[1:3], 7)
	binary.LittleEndian.PutUint32(raw[3:7], uint32(len(raw)))
	copy(raw[15:], body)

	plain, ok := plainRecvBody(raw, false)
	if !ok || !bytes.Equal(plain, body) {
		t.Fatalf("plainRecvBody = %x ok=%t", plain, ok)
	}
	response, typ, ok := buildPeerResponse(plain)
	if !ok || typ != peerRequestParty || binary.LittleEndian.Uint16(response[:2]) != 0x1234 {
		t.Fatalf("buildPeerResponse response=%x typ=%d ok=%t", response, typ, ok)
	}
}

func TestSelectPeerResponsePacketSupportsEncryptedAndPlainBodies(t *testing.T) {
	cipher := newPartyTestCipher(t)
	body := []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0xaa}

	plain := makePartyRecvPacket(7, body)
	response, typ, source, err := selectPeerResponsePacket(cipher, plain, false)
	if err != nil || source != "plain" || typ != peerRequestParty || binary.LittleEndian.Uint16(response[:2]) != 0x1234 {
		t.Fatalf("plain response=%x typ=%d source=%q err=%v", response, typ, source, err)
	}

	encrypted := makeEncryptedPartyRecvPacket(t, cipher, 7, body)
	response, typ, source, err = selectPeerResponsePacket(cipher, encrypted, false)
	if err != nil || source != "decrypted" || typ != peerRequestParty || binary.LittleEndian.Uint16(response[:2]) != 0x1234 {
		t.Fatalf("encrypted response=%x typ=%d source=%q err=%v", response, typ, source, err)
	}
}

func TestPartyIPInfoPacketSupportsEncryptedAndPlainBodies(t *testing.T) {
	cipher := newPartyTestCipher(t)
	body := make([]byte, 45)
	body[0] = 2
	putPartyPeer(body[1:23], 0x1111, net.IPv4(192, 168, 200, 1), 5063, 18000000, 1, 1472)
	putPartyPeer(body[23:45], 0x2222, net.IPv4(192, 168, 200, 131), 45678, 17000001, 1, 1472)

	for _, tt := range []struct {
		name   string
		packet []byte
		source string
	}{
		{name: "plain", packet: makePartyRecvPacket(11, body), source: "plain"},
		{name: "encrypted", packet: makeEncryptedPartyRecvPacket(t, cipher, 11, padPartyBlock(body)), source: "decrypted"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			self, peers, source, err := selectPartyIPInfoPacket(cipher, tt.packet, false, 17000001)
			if err != nil || source != tt.source || self.accID != 17000001 || len(peers) != 1 || peers[0].accID != 18000000 {
				t.Fatalf("self=%+v peers=%+v source=%q err=%v", self, peers, source, err)
			}
		})
	}
}

func TestPartyInfoPacketSupportsEncryptedAndPlainZlib(t *testing.T) {
	cipher := newPartyTestCipher(t)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(mustPartyHex(t, "0100220002ffffff486b01ffffffffffff00010000005887dd13")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name   string
		packet []byte
		source string
	}{
		{name: "plain", packet: makePartyRecvPacket(9, compressed.Bytes()), source: "plain"},
		{name: "encrypted", packet: makeEncryptedPartyRecvPacket(t, cipher, 9, padPartyBlock(compressed.Bytes())), source: "decrypted"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clears, source, err := partyInfoPacketClearsParty(cipher, tt.packet, false)
			if err != nil || !clears || source != tt.source {
				t.Fatalf("clears=%t source=%q err=%v", clears, source, err)
			}
		})
	}
}

func TestParseRecvPacketDoesNotTreatDamagedCiphertextAsPlain(t *testing.T) {
	cipher := newPartyTestCipher(t)
	packet := makePartyRecvPacket(22, []byte{1, 2, 3, 4, 5, 6, 7})
	if _, _, _, err := parseRecvPacket(cipher, packet, false); err == nil {
		t.Fatal("damaged ciphertext was accepted as a decrypted packet")
	}
}

func TestTownNotifySelectionPrefersKnownPlainCandidate(t *testing.T) {
	cipher := newPartyTestCipher(t)
	vo := &RobotVo{}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1234, slot: 0, slotKnown: true}

	position := make([]byte, 16)
	binary.LittleEndian.PutUint16(position[:2], 0x1234)
	binary.LittleEndian.PutUint16(position[2:4], 100)
	binary.LittleEndian.PutUint16(position[4:6], 200)
	position[6] = 5
	binary.LittleEndian.PutUint16(position[7:9], 120)
	body, ok := vo.selectTownEntityPositionBodyUnsafe(cipher, makePartyRecvPacket(22, position), false)
	if !ok || binary.LittleEndian.Uint16(body[:2]) != 0x1234 {
		t.Fatalf("selected position=%x ok=%t", body, ok)
	}

	area := make([]byte, 8)
	binary.LittleEndian.PutUint16(area[:2], 0x1234)
	area[2], area[3] = 3, 4
	binary.LittleEndian.PutUint16(area[4:6], 300)
	binary.LittleEndian.PutUint16(area[6:8], 400)
	selectedArea, ok := vo.selectTownEntityAreaUnsafe(cipher, makePartyRecvPacket(23, area), false)
	if !ok || selectedArea.uniqueID != 0x1234 || selectedArea.village != 3 || selectedArea.area != 4 {
		t.Fatalf("selected area=%+v ok=%t", selectedArea, ok)
	}
}
