package crypt

import (
	"encoding/hex"
	"testing"
)

func TestTwoFishMatchesCppVector(t *testing.T) {
	key, err := hex.DecodeString("da89040d2262d75973f61a77fe9002e8bc507d8c3c1cb7eb87757ba8c83a9ca2")
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("a9fe25f5c23c52d6fc714add3d10a50d")
	if err != nil {
		t.Fatal(err)
	}

	c := NewTwoFishCipher()
	if err := c.SetKey(key); err != nil {
		t.Fatal(err)
	}
	got, err := c.Encrypt(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("ciphertext mismatch: got %x want %x", got, want)
	}
}
