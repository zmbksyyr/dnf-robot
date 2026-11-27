package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The original ordinary-character create call (0x878470) produced this clear
// record, then its original sender encoded it and wrote the checksum. The
// frozen call stopped before network dispatch. Unlike captured headers whose
// last three bytes can be replaced by a token, this observes the full checksum.
// Sources: support-origin-create1/trace-native-create24201-crc2.json and
// trace-native-create24241-crc1.json / trace-native-create24242-crc1.json.
// The script data are explicit inputs from earlier captures, including the
// support forms; these calls do not prove skill eligibility or execution.
func TestPassiveObjectCodecMatchesOriginalCreateAndEncode(t *testing.T) {
	for _, tc := range []struct {
		name, data, native string
		key                uint32
	}{
		{"ordinary-24201", "000011010100ffff00000100895e00000100000000f0c94b00015a0300003f010000000000000a0000003d000000d20000000d020000000100", "0215009a510f81fac2f817f5852cd2e80ba65b59494eb7a02f9c6e42aff19685bf2ab90b7dde7516fac2e92df48fd35be836a7b7d0c54e6ea1229edd425f3900fac2e916010046000000", 0x16e9c2fa},
		{"support-main-24241", "000011010100ffff00000100b15e00000100000000f0c94b00015a0300003f0100000000000018000000f40100008a0290012c010100a0000300b004ba00000005000000000100", "021500c57997b4450386dd8a06d146170d5d729f44b8e45c3470c9bb9829d977d09a26eea2be4adc450397b98b1e2e7217f95de42e90ba595c187193bbc8e0267761c4f1eea28192dc4503000100450397dc010054000000", 0xdc970345},
		{"support-target-24242", "000011010100ffff00000100b25e00000100000000f0c94b00015a0300003f010000000000001000000005c11102b0040000ba000000050000000000000100", "02150052edccbc010c921d0318f9c604300d72ba3e18e410c030c82370a8db470199238e00b9401c010c8339020806720435cdf50ad01cc8117a3090238561204700c3000100010c831c01004c000000", 0x1c830c01},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mustPartyHex(t, tc.data)
			native := mustPartyHex(t, tc.native)
			// The snapshot precedes the transport's outer byte XOR. Preserve
			// the original word mask, footer and full checksum as the oracle.
			wire := bytes.Clone(native)
			for i := 7; i < len(wire); i++ {
				wire[i] ^= 0x7e
			}
			encoded, ok := buildPartyPassiveObjectBody(data, 1, tc.key)
			if !ok || !bytes.Equal(encoded, wire) {
				t.Fatalf("encoded=%x want original=%x ok=%t", encoded, wire, ok)
			}
			decoded, sender, ok := decodePartyPassiveObjectBody(wire)
			if !ok || sender != 1 || !bytes.Equal(decoded, data) {
				t.Fatalf("decoded=%x want=%x sender=%d ok=%t", decoded, data, sender, ok)
			}
		})
	}
}

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
