package crypt

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestTeaProtocolXTEAKnownAnswer(t *testing.T) {
	// XTEA, 32 cycles, big-endian words: all-zero 128-bit key and 64-bit block.
	// A fixed ciphertext checks wire compatibility; a round trip alone would
	// also accept two mutually wrong implementations.
	want, err := hex.DecodeString("dee9d4d8f7131ed9")
	if err != nil {
		t.Fatal(err)
	}
	cipher := NewTeaCipher()
	if err := cipher.SetKey(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	got, err := cipher.Encrypt(make([]byte, 8))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("XTEA encrypt = %x, %v; want %x", got, err, want)
	}
	plain, err := cipher.Decrypt(want)
	if err != nil || !bytes.Equal(plain, make([]byte, 8)) {
		t.Fatalf("XTEA decrypt = %x, %v; want zero block", plain, err)
	}
}

func TestTeaProtocolOriginalKeyedAnswer(t *testing.T) {
	// Independently captured from the pinned original ELF CTEA API:
	// key bytes 00..0f and plaintext ABCDEFGH. Nonzero key words also
	// catch Go's equal precedence of + and ^ in the round expression.
	key := make([]byte, 16)
	for i := range key {
		key[i] = byte(i)
	}
	want, err := hex.DecodeString("497df3d072612cb5")
	if err != nil {
		t.Fatal(err)
	}
	cipher := NewTeaCipher()
	if err := cipher.SetKey(key); err != nil {
		t.Fatal(err)
	}
	got, err := cipher.Encrypt([]byte("ABCDEFGH"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("keyed XTEA encrypt = %x, %v; want %x", got, err, want)
	}
	plain, err := cipher.Decrypt(want)
	if err != nil || !bytes.Equal(plain, []byte("ABCDEFGH")) {
		t.Fatalf("keyed XTEA decrypt = %x, %v; want ABCDEFGH", plain, err)
	}
}
