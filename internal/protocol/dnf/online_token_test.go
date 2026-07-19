package dnf

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"
)

func TestResolveLoginTokenKeepsValidExistingToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	existing := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, key.Size()))
	if got := resolveLoginToken(existing, 17000001, key); got != existing {
		t.Fatal("valid existing token was regenerated")
	}
}

func TestResolveLoginTokenReplacesMalformedToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	got := resolveLoginToken("not-base64", 17000001, key)
	if got == "" || got == "not-base64" {
		t.Fatalf("malformed token replacement = %q", got)
	}
	if !validLoginToken(got, key) {
		t.Fatal("generated replacement token is invalid")
	}
}
