package crypt

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestAnubisLibTomCryptOriginalVectors(t *testing.T) {
	tests := []struct {
		keySize   int
		cipherHex string
	}{
		{keySize: 16, cipherHex: "f06860fc6730e818f132c78af4132afe"},
		{keySize: 20, cipherHex: "bd5e32be5167a8e272d7950f83c68c31"},
		{keySize: 24, cipherHex: "17ac57449d596166d0c79e047cc758f0"},
		{keySize: 28, cipherHex: "a2f0a6b917932a3bef08e87a58d6f853"},
		{keySize: 32, cipherHex: "e086ac456b3ce513edf5dfddd63b7193"},
		{keySize: 36, cipherHex: "e8f4af2b21a0879b4195b9717579047c"},
		{keySize: 40, cipherHex: "1704d72cc68576024bcc3980d822eaa4"},
	}
	for _, tt := range tests {
		cipher := NewAnubisCipher()
		key := make([]byte, tt.keySize)
		key[0] = 0x80
		if err := cipher.SetKey(key); err != nil {
			t.Fatalf("SetKey(%d): %v", tt.keySize, err)
		}
		plain := make([]byte, cipher.BlockSize())
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
