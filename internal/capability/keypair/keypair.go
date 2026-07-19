package keypair

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/foundation/rsakey"
)

//go:embed defaults/*.pem
var defaultFiles embed.FS

type RuntimeKeySink func(*rsa.PrivateKey)

var runtimeKeySink struct {
	mu   sync.RWMutex
	sink RuntimeKeySink
}

type statusCacheKey struct {
	configDir string
	dfGameR   string
	files     [4]keypairFileStamp
	loaded    bool
}

type keypairFileStamp struct {
	path    string
	exists  bool
	size    int64
	modTime int64
}

var currentStatusCache struct {
	mu     lockhub.Locker
	valid  bool
	key    statusCacheKey
	status KeypairStatus
}

func SetRuntimeKeySink(sink RuntimeKeySink) {
	runtimeKeySink.mu.Lock()
	runtimeKeySink.sink = sink
	runtimeKeySink.mu.Unlock()
}

func publishRuntimeKey(key *rsa.PrivateKey) {
	runtimeKeySink.mu.RLock()
	sink := runtimeKeySink.sink
	runtimeKeySink.mu.RUnlock()
	if sink != nil {
		sink(key)
	}
}

type KeypairStatus struct {
	Loaded             bool   `json:"loaded"`
	Source             string `json:"source,omitempty"`
	KeyState           string `json:"key_state"`
	KeyStateLabel      string `json:"key_state_label"`
	KeyReason          string `json:"key_reason,omitempty"`
	Fingerprint        string `json:"fingerprint,omitempty"`
	ConfigPrivate      string `json:"config_private,omitempty"`
	ConfigPublic       string `json:"config_public,omitempty"`
	GamePrivate        string `json:"game_private,omitempty"`
	GamePublic         string `json:"game_public,omitempty"`
	ConfigValid        bool   `json:"config_valid"`
	GameValid          bool   `json:"game_valid"`
	UsingDefault       bool   `json:"using_default"`
	DefaultValid       bool   `json:"default_valid"`
	CanReleaseDefault  bool   `json:"can_release_default"`
	CanDownloadCurrent bool   `json:"can_download_current"`
	Error              string `json:"error,omitempty"`
}

func EnsureRuntimeKeypair(cfg *config.SysConfig) {
	if cfg == nil || cfg.ConfigDir == "" || cfg.DFGameR == "" {
		return
	}
	gamePriv, gamePub := keypairGamePaths(cfg)
	if pair, _, err := ensureGameKeypair(cfg); err == nil {
		if err := copyKeypairToConfig(cfg.ConfigDir, gamePriv, gamePub, pair); err != nil {
			foundationlog.Robotf("KEYPAIR_CONFIG_SYNC_FAILED err=%v\n", err)
		}
		invalidateStatusCache()
		return
	} else {
		foundationlog.Robotf("KEYPAIR_GAME_INVALID private=%s public=%s err=%v action=blocked_until_release_default\n", gamePriv, gamePub, err)
	}
}

func ReleaseDefault(cfg *config.SysConfig) (KeypairStatus, error) {
	invalidateStatusCache()
	if err := writeDefaultKeypairToGameAndConfig(cfg); err != nil {
		return BuildKeypairStatus(cfg), err
	}
	cfgPriv, _ := keypairConfigPaths(cfg.ConfigDir)
	if err := InitPrivateKey(cfgPriv); err != nil {
		return BuildKeypairStatus(cfg), err
	}
	publishRuntimeKey(GetRSAKey())
	st := BuildKeypairStatus(cfg)
	st.Loaded = GetRSAKey() != nil
	invalidateStatusCache()
	foundationlog.Robotf("KEYPAIR_DEFAULT_RELEASED game_private=%s game_public=%s fingerprint=%s\n", st.GamePrivate, st.GamePublic, st.Fingerprint)
	return st, nil
}

func CurrentStatus(cfg *config.SysConfig) KeypairStatus {
	currentStatusCache.mu.Lock()
	defer currentStatusCache.mu.Unlock()
	key := currentStatusCacheKey(cfg)
	if currentStatusCache.valid && currentStatusCache.key == key {
		return currentStatusCache.status
	}
	st := BuildKeypairStatus(cfg)
	st.Loaded = GetRSAKey() != nil
	currentStatusCache.key = currentStatusCacheKey(cfg)
	currentStatusCache.status = st
	currentStatusCache.valid = true
	return st
}

func currentStatusCacheKey(cfg *config.SysConfig) statusCacheKey {
	key := statusCacheKey{loaded: GetRSAKey() != nil}
	if cfg == nil {
		return key
	}
	key.configDir = cfg.ConfigDir
	key.dfGameR = cfg.DFGameR
	cfgPriv, cfgPub := keypairConfigPaths(cfg.ConfigDir)
	gamePriv, gamePub := keypairGamePaths(cfg)
	paths := [...]string{cfgPriv, cfgPub, gamePriv, gamePub}
	for i, path := range paths {
		key.files[i] = statKeypairFile(path)
	}
	return key
}

func statKeypairFile(path string) keypairFileStamp {
	stamp := keypairFileStamp{path: path}
	info, err := os.Stat(path)
	if err != nil {
		return stamp
	}
	stamp.exists = true
	stamp.size = info.Size()
	stamp.modTime = info.ModTime().UnixNano()
	return stamp
}

func invalidateStatusCache() {
	currentStatusCache.mu.Lock()
	currentStatusCache.valid = false
	currentStatusCache.mu.Unlock()
}

func BuildKeypairStatus(cfg *config.SysConfig) KeypairStatus {
	st := KeypairStatus{KeyState: "missing", KeyStateLabel: "缺失"}
	if cfg == nil {
		st.Error = "nil config"
		st.KeyReason = "robot 配置为空，无法定位 game 目录"
		return st
	}
	cfgPriv, cfgPub := keypairConfigPaths(cfg.ConfigDir)
	gamePriv, gamePub := keypairGamePaths(cfg)
	st.ConfigPrivate, st.ConfigPublic = cfgPriv, cfgPub
	st.GamePrivate, st.GamePublic = gamePriv, gamePub
	defaultPair, defaultErr := loadDefaultKeypair()
	st.DefaultValid = defaultErr == nil
	if pair, reason, err := ensureGameKeypair(cfg); err == nil {
		if err := copyKeypairToConfig(cfg.ConfigDir, gamePriv, gamePub, pair); err != nil {
			foundationlog.Robotf("KEYPAIR_CONFIG_SYNC_FAILED err=%v\n", err)
		}
		st.GameValid = true
		st.Fingerprint = pair.fingerprint
		st.UsingDefault = defaultErr == nil && pair.fingerprint == defaultPair.fingerprint
		if st.UsingDefault {
			st.Source = "default"
			st.KeyState = "default"
			st.KeyStateLabel = "默认"
		} else {
			st.Source = "game_dir"
			st.KeyState = "user"
			st.KeyStateLabel = "用户"
		}
		st.KeyReason = reason
	} else {
		st.KeyReason = reason
		st.Error = err.Error()
		if keypairFileExists(gamePriv) || keypairFileExists(gamePub) {
			st.KeyState = "invalid"
			st.KeyStateLabel = "无效"
		}
	}
	if _, err := loadKeypair(cfgPriv, cfgPub); err == nil {
		st.ConfigValid = true
	} else if st.Error == "" && keypairFileExists(cfgPriv) {
		st.Error = err.Error()
	}
	st.CanReleaseDefault = st.KeyState == "missing" || st.KeyState == "invalid"
	st.CanDownloadCurrent = st.GameValid
	return st
}

type loadedKeypair struct {
	private     *rsa.PrivateKey
	public      *rsa.PublicKey
	fingerprint string
}

func loadKeypair(privatePath, publicPath string) (loadedKeypair, error) {
	priv, err := parseRSAPrivateFile(privatePath)
	if err != nil {
		return loadedKeypair{}, err
	}
	pub, err := parseRSAPublicFile(publicPath)
	if err != nil {
		return loadedKeypair{}, err
	}
	if priv.N.Cmp(pub.N) != 0 || priv.E != pub.E {
		return loadedKeypair{}, fmt.Errorf("private/public key mismatch")
	}
	return validateKeypair(priv, pub)
}

func ensureGameKeypair(cfg *config.SysConfig) (loadedKeypair, string, error) {
	gamePriv, gamePub := keypairGamePaths(cfg)
	privExists := keypairFileExists(gamePriv)
	pubExists := keypairFileExists(gamePub)
	if !privExists {
		if pubExists {
			reason := "game目录当前密钥缺少 privatekey.pem，无法从 publickey.pem 还原私钥"
			return loadedKeypair{}, reason, errors.New(reason)
		}
		reason := "game目录当前密钥缺少 privatekey.pem 和 publickey.pem"
		return loadedKeypair{}, reason, errors.New(reason)
	}
	priv, err := parseRSAPrivateFile(gamePriv)
	if err != nil {
		reason := "game目录当前 privatekey.pem 解析失败，不是合法 RSA 私钥"
		return loadedKeypair{}, reason, fmt.Errorf("%s: %w", reason, err)
	}
	if !pubExists {
		pubPEM, err := marshalRSAPublicPEM(&priv.PublicKey)
		if err != nil {
			reason := "game目录当前 privatekey.pem 导出 publickey.pem 失败"
			return loadedKeypair{}, reason, fmt.Errorf("%s: %w", reason, err)
		}
		if err := os.WriteFile(gamePub, pubPEM, 0644); err != nil {
			reason := "game目录当前 privatekey.pem 合法，但写入 publickey.pem 失败"
			return loadedKeypair{}, reason, fmt.Errorf("%s: %w", reason, err)
		}
		_ = os.Chmod(gamePub, 0644)
		foundationlog.Robotf("KEYPAIR_PUBLIC_DERIVED game_public=%s source_private=%s\n", gamePub, gamePriv)
		pair, err := validateKeypair(priv, &priv.PublicKey)
		return pair, "game目录当前 privatekey.pem 合法，已自动导出 publickey.pem", err
	}
	pub, err := parseRSAPublicFile(gamePub)
	if err != nil {
		reason := "game目录当前 publickey.pem 解析失败，不是合法 RSA 公钥"
		return loadedKeypair{}, reason, fmt.Errorf("%s: %w", reason, err)
	}
	if priv.N.Cmp(pub.N) != 0 || priv.E != pub.E {
		reason := "game目录当前 privatekey.pem 与 publickey.pem 不是同一对 RSA 密钥"
		return loadedKeypair{}, reason, errors.New(reason)
	}
	pair, err := validateKeypair(priv, pub)
	if err != nil {
		reason := "game目录当前密钥对验签失败"
		return loadedKeypair{}, reason, fmt.Errorf("%s: %w", reason, err)
	}
	return pair, "game目录当前密钥对合法", nil
}

func validateKeypair(priv *rsa.PrivateKey, pub *rsa.PublicKey) (loadedKeypair, error) {
	if priv == nil || pub == nil {
		return loadedKeypair{}, fmt.Errorf("nil key")
	}
	if priv.N.Cmp(pub.N) != 0 || priv.E != pub.E {
		return loadedKeypair{}, fmt.Errorf("private/public key mismatch")
	}
	probe := []byte("tw-robot-keypair-check")
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, 0, probe)
	if err != nil {
		return loadedKeypair{}, err
	}
	if err := rsa.VerifyPKCS1v15(pub, 0, probe, sig); err != nil {
		return loadedKeypair{}, err
	}
	der, _ := x509.MarshalPKIXPublicKey(pub)
	sum := sha256.Sum256(der)
	return loadedKeypair{private: priv, public: pub, fingerprint: hex.EncodeToString(sum[:])}, nil
}

func parseRSAPrivateFile(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRSAPrivatePEM(data)
}

func parseRSAPrivatePEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key PEM decode failed")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}

func parseRSAPublicFile(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRSAPublicPEM(data)
}

func parseRSAPublicPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("public key PEM decode failed")
	}
	if pubAny, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if pub, ok := pubAny.(*rsa.PublicKey); ok {
			return pub, nil
		}
		return nil, fmt.Errorf("public key is not RSA")
	}
	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	return nil, fmt.Errorf("failed to parse RSA public key")
}

func marshalRSAPublicPEM(pub *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func copyKeypairToConfig(configDir, srcPriv, srcPub string, pair loadedKeypair) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	dstPriv, dstPub := keypairConfigPaths(configDir)
	if err := copyFileIfDifferent(srcPriv, dstPriv, 0600); err != nil {
		return err
	}
	if err := copyFileIfDifferent(srcPub, dstPub, 0644); err != nil {
		return err
	}
	return nil
}

func copyFileIfDifferent(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(dst); err == nil && bytes.Equal(current, data) {
		return os.Chmod(dst, mode)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func keypairConfigPaths(configDir string) (string, string) {
	return filepath.Join(configDir, "privatekey.pem"), filepath.Join(configDir, "publickey.pem")
}

func keypairGamePaths(cfg *config.SysConfig) (string, string) {
	gameDir := ""
	if cfg != nil && cfg.DFGameR != "" {
		gameDir = filepath.Dir(cfg.DFGameR)
	}
	return filepath.Join(gameDir, "privatekey.pem"), filepath.Join(gameDir, "publickey.pem")
}

func keypairFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadDefaultKeypair() (loadedKeypair, error) {
	priv, err := defaultFiles.ReadFile("defaults/privatekey.pem")
	if err != nil {
		return loadedKeypair{}, err
	}
	pub, err := defaultFiles.ReadFile("defaults/publickey.pem")
	if err != nil {
		return loadedKeypair{}, err
	}
	return loadKeypairBytes(priv, pub)
}

func DefaultKeypairPEM() ([]byte, []byte, error) {
	priv, err := defaultFiles.ReadFile("defaults/privatekey.pem")
	if err != nil {
		return nil, nil, err
	}
	pub, err := defaultFiles.ReadFile("defaults/publickey.pem")
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

func loadKeypairBytes(privatePEM, publicPEM []byte) (loadedKeypair, error) {
	priv, err := parseRSAPrivatePEM(privatePEM)
	if err != nil {
		return loadedKeypair{}, err
	}
	pub, err := parseRSAPublicPEM(publicPEM)
	if err != nil {
		return loadedKeypair{}, err
	}
	return validateKeypair(priv, pub)
}

func writeDefaultKeypairToGameAndConfig(cfg *config.SysConfig) error {
	priv, err := defaultFiles.ReadFile("defaults/privatekey.pem")
	if err != nil {
		return err
	}
	pub, err := defaultFiles.ReadFile("defaults/publickey.pem")
	if err != nil {
		return err
	}
	gamePriv, gamePub := keypairGamePaths(cfg)
	cfgPriv, cfgPub := keypairConfigPaths(cfg.ConfigDir)
	if err := os.MkdirAll(filepath.Dir(gamePriv), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(gamePriv, priv, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(gamePub, pub, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(cfgPriv, priv, 0600); err != nil {
		return err
	}
	return os.WriteFile(cfgPub, pub, 0644)
}

func InitPrivateKey(keyFile string) error {
	err := rsakey.InitPrivateKey(keyFile)
	invalidateStatusCache()
	return err
}

func ClosePrivateKey() {
	rsakey.ClosePrivateKey()
	invalidateStatusCache()
}

func GetRSAKey() *rsa.PrivateKey {
	return rsakey.GetRSAKey()
}
