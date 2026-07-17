package crypt

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDecryptAntiSupportsProtocolCryptoTypes(t *testing.T) {
	for _, cryptoType := range []uint32{18, 19} {
		t.Run(string(rune('0'+cryptoType-10)), func(t *testing.T) {
			inner := make([]byte, 15)
			inner[0] = 0
			binary.LittleEndian.PutUint16(inner[1:3], 7)
			binary.LittleEndian.PutUint32(inner[3:7], uint32(len(inner)))

			plain := make([]byte, 9+len(inner))
			copy(plain[9:], inner)
			seed := uint32(0x039e3fdc)
			encrypted := append([]byte(nil), plain...)
			GeneNew(GenKey(int(cryptoType), seed), true, encrypted, len(encrypted))

			packet := make([]byte, 17+len(encrypted))
			binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
			binary.BigEndian.PutUint16(packet[2:4], 17)
			binary.BigEndian.PutUint32(packet[5:9], seed)
			binary.BigEndian.PutUint32(packet[9:13], cryptoType)
			copy(packet[17:], encrypted)

			got, err := new(DNFCipher).DecryptAnti(packet)
			if err != nil {
				t.Fatalf("DecryptAnti() error = %v", err)
			}
			if !bytes.Equal(got, inner) {
				t.Fatalf("DecryptAnti() = %x, want %x", got, inner)
			}
		})
	}
}

func TestDecryptAntiRejectsInvalidProtocol(t *testing.T) {
	packet := make([]byte, 41)
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[2:4], 16)
	binary.BigEndian.PutUint32(packet[9:13], 18)
	if _, err := new(DNFCipher).DecryptAnti(packet); err == nil {
		t.Fatal("DecryptAnti() accepted invalid protocol")
	}
}
