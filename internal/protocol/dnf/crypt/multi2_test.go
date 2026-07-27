package crypt

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestMulti2OfficialVector(t *testing.T) {
	key := make([]byte, 40)
	copy(key[32:], []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	plain := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	want, err := hex.DecodeString("f89440845e11cf89")
	if err != nil {
		t.Fatal(err)
	}

	cipher := NewMulti2Cipher()
	if err := cipher.SetKey(key); err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encrypted, want) {
		t.Fatalf("Encrypt() = %x, want %x", encrypted, want)
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("Decrypt() = %x, want %x", decrypted, plain)
	}
}

func TestMulti2RequiresKey(t *testing.T) {
	cipher := NewMulti2Cipher()
	if _, err := cipher.Encrypt(make([]byte, cipher.BlockSize())); err != ErrInvalidKeySize {
		t.Fatalf("Encrypt() error = %v, want %v", err, ErrInvalidKeySize)
	}
	if _, err := cipher.Decrypt(make([]byte, cipher.BlockSize())); err != ErrInvalidKeySize {
		t.Fatalf("Decrypt() error = %v, want %v", err, ErrInvalidKeySize)
	}
}
