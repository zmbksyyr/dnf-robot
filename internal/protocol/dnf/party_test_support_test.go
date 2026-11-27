package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"

	"robot/internal/protocol/dnf/crypt"
)

// Collect bodies only for assertions. Production observes them in place.
func partyDungeonFrameBodies(frame []byte) ([][]byte, bool) {
	if len(frame) < 9 || len(frame) != 9+int(binary.LittleEndian.Uint16(frame[5:7])) {
		return nil, false
	}
	payload := frame[9:]
	if frame[0] == 2 {
		return [][]byte{payload}, true
	}
	if frame[0] != 1 {
		return nil, false
	}
	var bodies [][]byte
	for len(payload) >= 2 {
		size := int(binary.LittleEndian.Uint16(payload))
		payload = payload[2:]
		if size == 0 || size > len(payload) {
			return nil, false
		}
		bodies = append(bodies, payload[:size])
		payload = payload[size:]
	}
	return bodies, len(payload) == 0
}

func putPartyPeer(dst []byte, uniqueID uint16, ip net.IP, port uint16, accID uint32, natType byte, mtu uint32) {
	binary.LittleEndian.PutUint16(dst[:2], uniqueID)
	copy(dst[2:6], ip.To4())
	copy(dst[6:10], ip.To4())
	binary.BigEndian.PutUint16(dst[10:12], port)
	binary.LittleEndian.PutUint32(dst[12:16], accID)
	dst[16] = natType
	binary.LittleEndian.PutUint32(dst[17:21], mtu)
}

func mustPartyHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode %q: %v", value, err)
	}
	return data
}

func newPartyTestCipher(t *testing.T) *crypt.DNFCipher {
	t.Helper()
	cipher := crypt.NewDNFCipher()
	if err := cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatalf("initialize cipher: %v", err)
	}
	return cipher
}

func parsePartyTQOSPacket(payload []byte, expectedRoute byte) (partyTQOSPacket, bool) {
	return parsePartyTQOSPacketWithCodec(payload, expectedRoute, nil)
}

func selectPeerResponsePacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool, preferred recvBodySource, knownUniqueID func(uint16) bool) ([]byte, byte, recvBodySource, error) {
	selected, _, err := selectPeerResponsePackets(cipher, raw, isAnti, preferred, knownUniqueID)
	return selected.data, selected.typ, selected.source, err
}

func makePartyRecvPacket(packetType uint16, body []byte) []byte {
	packet := make([]byte, 15+len(body))
	binary.LittleEndian.PutUint16(packet[1:3], packetType)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	copy(packet[15:], body)
	return packet
}

func makeEncryptedPartyRecvPacket(t *testing.T, cipher *crypt.DNFCipher, packetType uint16, body []byte) []byte {
	t.Helper()
	encrypted, err := cipher.Encrypt(packetType, body)
	if err != nil {
		t.Fatalf("encrypt packet type %d: %v", packetType, err)
	}
	return makePartyRecvPacket(packetType, encrypted)
}

func padPartyBlock(body []byte) []byte {
	padded := make([]byte, alignTo(len(body), 8))
	copy(padded, body)
	return padded
}

func readPartyRelayPacket(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	header := make([]byte, 12)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Errorf("read relay header: %v", err)
		return nil
	}
	size := int(binary.LittleEndian.Uint16(header[2:4]))
	if size < len(header) {
		t.Errorf("invalid relay size %d", size)
		return nil
	}
	packet := make([]byte, size)
	copy(packet, header)
	if _, err := io.ReadFull(conn, packet[len(header):]); err != nil {
		t.Errorf("read relay body: %v", err)
		return nil
	}
	return packet
}

func assertPartyRelayPayload(t *testing.T, packet []byte, src, dst uint32) {
	t.Helper()
	if len(packet) < 21 || binary.LittleEndian.Uint16(packet[:2]) != 1 || binary.LittleEndian.Uint32(packet[4:8]) != src || binary.LittleEndian.Uint32(packet[8:12]) != dst {
		t.Fatalf("relay packet=%x src=%d dst=%d", packet, src, dst)
	}
}
