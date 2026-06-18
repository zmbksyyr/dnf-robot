package crypt

import (
	"bytes"
	"testing"
)

func TestRc6EncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 60)
	for i := range key {
		key[i] = byte(i*7 + 3)
	}
	plain := []byte{
		0x50, 0x24, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05,
		0x00, 0x00, 0x00, 'h', 'e', 'l', 'l', 'o',
	}

	c := NewRc6Cipher()
	if err := c.SetKey(key); err != nil {
		t.Fatalf("SetKey() error = %v", err)
	}
	encrypted, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	decrypted, err := c.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("Decrypt(Encrypt(plain)) = %x, want %x", decrypted, plain)
	}
}
