package crypt

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestAnubisKernelVectors(t *testing.T) {
	tests := []struct {
		keyByte   byte
		keySize   int
		plainByte byte
		cipherHex string
	}{
		{keyByte: 0xfe, keySize: 16, plainByte: 0xfe, cipherHex: "6dc5daa2267d626f08b7528e6e6e8690"},
		{keyByte: 0x03, keySize: 20, plainByte: 0x03, cipherHex: "dbf142f4d18ac74987416f820a9864ae"},
		{keyByte: 0x24, keySize: 28, plainByte: 0x24, cipherHex: "fd1b4ae3bff0ad3d06d36127fd139ede"},
		{keyByte: 0x25, keySize: 32, plainByte: 0x25, cipherHex: "1a91fb2bb7786bc417d9ff403b0ee5fe"},
		{keyByte: 0x35, keySize: 40, plainByte: 0x35, cipherHex: "a52c856f9cbaa0979ec6840f172107ee"},
	}
	for _, tt := range tests {
		cipher := NewAnubisCipher()
		key := bytes.Repeat([]byte{tt.keyByte}, tt.keySize)
		if err := cipher.SetKey(key); err != nil {
			t.Fatalf("SetKey(%d): %v", tt.keySize, err)
		}
		plain := bytes.Repeat([]byte{tt.plainByte}, cipher.BlockSize())
		want, err := hex.DecodeString(tt.cipherHex)
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := cipher.Encrypt(plain)
		if err != nil || !bytes.Equal(encrypted, want) {
			t.Fatalf("Encrypt key_size=%d = %x, %v; want %x", tt.keySize, encrypted, err, want)
		}
		decrypted, err := cipher.Decrypt(encrypted)
		if err != nil || !bytes.Equal(decrypted, plain) {
			t.Fatalf("Decrypt key_size=%d = %x, %v; want %x", tt.keySize, decrypted, err, plain)
		}
	}
}

func TestAnubisRejectsInvalidSizes(t *testing.T) {
	cipher := NewAnubisCipher()
	for _, size := range []int{0, 15, 17, 41} {
		if err := cipher.SetKey(make([]byte, size)); err != ErrInvalidKeySize {
			t.Fatalf("SetKey(%d) error = %v, want %v", size, err, ErrInvalidKeySize)
		}
	}
	if err := cipher.SetKey(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Encrypt(make([]byte, 15)); err != ErrInvalidBlockSize {
		t.Fatalf("Encrypt error = %v, want %v", err, ErrInvalidBlockSize)
	}
}

func TestAnubisRoundTripAllSupportedKeySizes(t *testing.T) {
	plain := []byte("0123456789abcdeffedcba9876543210")
	for _, size := range []int{16, 20, 24, 28, 32, 36, 40} {
		cipher := NewAnubisCipher()
		if err := cipher.SetKey(bytes.Repeat([]byte{byte(size)}, size)); err != nil {
			t.Fatalf("SetKey(%d): %v", size, err)
		}
		encrypted, err := cipher.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt key_size=%d: %v", size, err)
		}
		decrypted, err := cipher.Decrypt(encrypted)
		if err != nil || !bytes.Equal(decrypted, plain) {
			t.Fatalf("Decrypt key_size=%d = %x, %v; want %x", size, decrypted, err, plain)
		}
	}
}
