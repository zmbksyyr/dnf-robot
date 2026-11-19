package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Captured from the original graphical client in the isolated test instance.
// The first skill script independently names passive object 24201 and writes
// two dwords plus one word. Other forms have different coordinate/data layouts.
func TestPassiveObjectCodecMatchesOriginalClientCaptures(t *testing.T) {
	for _, tc := range []struct {
		name, wire, source string
		code, key          uint32
		length             int
	}{
		{"player-windstrike", "02150076a12f1a4a854e970188c251ad92f8de50f9713f30cd61fce3e83f8f44b2d3720ae4d46f964a855fae17823ddeadbef93fd96e71fc3093607ae318407e4a855f96687e387e7e7e", "11011600", 24201, 0xe821fb34, 57},
		{"apc-skill", "021500fea73b88463042020fa9db7b9f46ca8aac501497f99daaac7149b38161f08d3440628fe903463053840eee248a9f43cb97bd0b14acf940a9db71b9d77e7f7e46305303687e367e7e7e", "1101014b", 24337, 0x7d2d4e38, 59},
		{"monster-projectile", "021500e482589045ca4d023d9bc47d03aff586b4a96b8f893b559c910592a3a0992c39c576276f007bcb5c82fc163b86f9aff48f7fdc6b9c393f55baf59128f6a169d36fc151245c0020ca21ca5c00687e2f7e7e7e", "1102fd8d", 30001, 0x7e22b45f, 68},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := mustPartyHex(t, tc.wire)
			data, sender, ok := decodePartyPassiveObjectBody(wire)
			if !ok || sender != 22 || len(data) != tc.length || !bytes.Equal(data[2:6], mustPartyHex(t, tc.source)) || binary.LittleEndian.Uint32(data[12:16]) != tc.code {
				t.Fatalf("decoded=%x sender=%d ok=%t", data, sender, ok)
			}
			if tc.code == 24201 {
				if binary.LittleEndian.Uint32(data[26:30]) != 1235 || binary.LittleEndian.Uint32(data[30:34]) != 343 || !bytes.Equal(data[42:52], mustPartyHex(t, "2c000000c90000005e01")) {
					t.Fatalf("windstrike script arguments or position differ: %x", data)
				}
			}
			if !bytes.Equal(data[len(data)-5:], mustPartyHex(t, "0000000100")) {
				t.Fatalf("partial-word tail was masked: %x", data)
			}
			encoded, ok := buildPartyPassiveObjectBody(data, sender, tc.key)
			// Reliable data records validate the first checksum byte; the next
			// three original header bytes contain a separate changing token.
			if !ok || !bytes.Equal(encoded[:4], wire[:4]) || !bytes.Equal(encoded[7:], wire[7:]) {
				t.Fatalf("encoded=%x want original=%x", encoded, wire)
			}
			for end := 0; end < len(wire); end++ {
				if _, _, ok := decodePartyPassiveObjectBody(wire[:end]); ok {
					t.Fatalf("accepted truncated record length %d", end)
				}
			}
			wire[len(wire)-1] ^= 1
			if _, _, ok := decodePartyPassiveObjectBody(wire); ok {
				t.Fatal("accepted a damaged original record")
			}
		})
	}
}
