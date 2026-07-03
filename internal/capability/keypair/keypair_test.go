package keypair

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"robot/internal/foundation/config"
)

func TestBuildKeypairStatusDerivesMissingPublicKey(t *testing.T) {
	cfg, gameDir := testKeypairConfig(t)
	priv := testPrivateKeyPEM(t)
	if err := os.WriteFile(filepath.Join(gameDir, "privatekey.pem"), priv, 0600); err != nil {
		t.Fatal(err)
	}

	st := BuildKeypairStatus(cfg)
	if !st.GameValid {
		t.Fatalf("expected game keypair to be valid after deriving public key: %+v", st)
	}
	if st.KeyState != "user" {
		t.Fatalf("expected user key state, got %q", st.KeyState)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "publickey.pem")); err != nil {
		t.Fatalf("expected derived public key in game dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "privatekey.pem")); err != nil {
		t.Fatalf("expected private key copied to config dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "publickey.pem")); err != nil {
		t.Fatalf("expected public key copied to config dir: %v", err)
	}
}

func TestBuildKeypairStatusRejectsMismatchedKeys(t *testing.T) {
	cfg, gameDir := testKeypairConfig(t)
	priv := testPrivateKeyPEM(t)
	otherPriv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, err := marshalRSAPublicPEM(&otherPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "privatekey.pem"), priv, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "publickey.pem"), otherPub, 0644); err != nil {
		t.Fatal(err)
	}

	st := BuildKeypairStatus(cfg)
	if st.GameValid {
		t.Fatalf("expected mismatched keypair to be invalid: %+v", st)
	}
	if st.KeyState != "invalid" {
		t.Fatalf("expected invalid key state, got %q", st.KeyState)
	}
	if !st.CanReleaseDefault {
		t.Fatalf("expected default key release to be available")
	}
}

func testKeypairConfig(t *testing.T) (*config.SysConfig, string) {
	t.Helper()
	root := t.TempDir()
	gameDir := filepath.Join(root, "game")
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	dfGameR := filepath.Join(gameDir, "df_game_r")
	if err := os.WriteFile(dfGameR, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	return &config.SysConfig{ConfigDir: configDir, DFGameR: dfGameR}, gameDir
}

func testPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
}
