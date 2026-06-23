package crypt

import (
	"encoding/hex"
	"testing"
)

func TestShiftCipherMatchesLegacyXorOnly(t *testing.T) {
	key, _ := hex.DecodeString("a1c4eba027a9907a")
	plain, _ := hex.DecodeString("49cce01e3fa745e0")
	wantCipher, _ := hex.DecodeString("6e657064180ed59a")

	c := NewShiftCipher()
	if err := c.SetKey(key); err != nil {
		t.Fatalf("SetKey() error = %v", err)
	}

	gotCipher, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if hex.EncodeToString(gotCipher) != hex.EncodeToString(wantCipher) {
		t.Fatalf("Encrypt() = %x, want %x", gotCipher, wantCipher)
	}

	gotPlain, err := c.Decrypt(gotCipher)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if hex.EncodeToString(gotPlain) != hex.EncodeToString(plain) {
		t.Fatalf("Decrypt(Encrypt()) = %x, want %x", gotPlain, plain)
	}
}
