package keypair

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestCurrentStatusRefreshesWhenKeyFilesChange(t *testing.T) {
	invalidateStatusCache()
	t.Cleanup(invalidateStatusCache)
	cfg, gameDir := testKeypairConfig(t)
	priv := testPrivateKeyPEM(t)
	if err := os.WriteFile(filepath.Join(gameDir, "privatekey.pem"), priv, 0600); err != nil {
		t.Fatal(err)
	}
	if st := CurrentStatus(cfg); !st.GameValid {
		t.Fatalf("initial status should be valid: %+v", st)
	}

	otherPriv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, err := marshalRSAPublicPEM(&otherPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(gameDir, "publickey.pem")
	if err := os.WriteFile(publicPath, otherPub, 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(publicPath, future, future); err != nil {
		t.Fatal(err)
	}
	if st := CurrentStatus(cfg); st.GameValid || st.KeyState != "invalid" {
		t.Fatalf("changed public key should invalidate cached status: %+v", st)
	}
}

func TestCurrentStatusCoalescesConcurrentValidation(t *testing.T) {
	invalidateStatusCache()
	t.Cleanup(invalidateStatusCache)
	cfg, gameDir := testKeypairConfig(t)
	if err := os.WriteFile(filepath.Join(gameDir, "privatekey.pem"), testPrivateKeyPEM(t), 0600); err != nil {
		t.Fatal(err)
	}

	const callers = 24
	var wg sync.WaitGroup
	statuses := make(chan KeypairStatus, callers)
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			statuses <- CurrentStatus(cfg)
		}()
	}
	wg.Wait()
	close(statuses)
	for st := range statuses {
		if !st.GameValid || st.Fingerprint == "" {
			t.Fatalf("concurrent status should be valid: %+v", st)
		}
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
