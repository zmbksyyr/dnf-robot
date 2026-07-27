package crypt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestDecryptAntiSupportsProtocolCryptoTypes(t *testing.T) {
	for _, orderCase := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{name: "big_endian", order: binary.BigEndian},
		{name: "little_endian", order: binary.LittleEndian},
	} {
		for cryptoType := uint32(0); cryptoType <= maxAntiCryptoType; cryptoType++ {
			t.Run(fmt.Sprintf("%s_%d", orderCase.name, cryptoType), func(t *testing.T) {
				packet, inner := antiTestPacket(orderCase.order, cryptoType)

				got, err := new(DNFCipher).DecryptAnti(packet)
				if err != nil {
					t.Fatalf("DecryptAnti() error = %v", err)
				}
				if !bytes.Equal(got, inner) {
					t.Fatalf("DecryptAnti() = %x, want %x", got, inner)
				}
				if cap(got) != len(got) {
					t.Fatalf("DecryptAnti() retained %d bytes of unused capacity", cap(got)-len(got))
				}
			})
		}
	}
}

func TestDecryptAntiAllocations(t *testing.T) {
	packet, _ := antiTestPacket(binary.BigEndian, 18)
	cipher := new(DNFCipher)
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := cipher.DecryptAnti(packet); err != nil {
			t.Fatalf("DecryptAnti() error = %v", err)
		}
	})
	if allocs > 6 {
		t.Fatalf("DecryptAnti() allocations = %.0f, want at most 6", allocs)
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

func TestDecryptAntiRejectsCryptoTypeOutsideProtocolRange(t *testing.T) {
	packet, _ := antiTestPacket(binary.BigEndian, maxAntiCryptoType+1)
	if _, err := new(DNFCipher).DecryptAnti(packet); err == nil {
		t.Fatal("DecryptAnti() accepted crypto type outside 0..19")
	}
}

func TestDecryptAntiRejectsShortEncryptedBody(t *testing.T) {
	packet := make([]byte, 18)
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[2:4], 17)
	binary.BigEndian.PutUint32(packet[9:13], 18)
	if _, err := new(DNFCipher).DecryptAnti(packet); err == nil {
		t.Fatal("DecryptAnti() accepted a body shorter than its inner header")
	}
}

func antiTestPacket(order binary.ByteOrder, cryptoType uint32) ([]byte, []byte) {
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
	order.PutUint16(packet[0:2], uint16(len(packet)))
	order.PutUint16(packet[2:4], 17)
	order.PutUint32(packet[5:9], seed)
	order.PutUint32(packet[9:13], cryptoType)
	copy(packet[17:], encrypted)
	return packet, inner
}
