package runtime

import (
	"bytes"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"robot/internal/capability/catalog"
	"robot/internal/capability/keypair"
	"robot/internal/capability/pvf"
	"robot/internal/foundation/config"
)

//go:embed defaults/*
var defaultFiles embed.FS

func Init(cfg *config.SysConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = "./config"
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		return err
	}
	if err := ensureConfigRuntimeFiles(cfg.ConfigDir); err != nil {
		return err
	}
	if err := catalog.LoadPartySkills(cfg.ConfigDir); err != nil {
		fmt.Printf("[Runtime] party skill catalog unavailable: %v\n", err)
	}
	keypair.EnsureRuntimeKeypair(cfg)
	if err := pvf.EnsureExports(cfg.DFGameR, cfg.ConfigDir); err != nil {
		return err
	}
	if err := updateRuntimeManifest(cfg); err != nil {
		fmt.Printf("[Runtime] self-check manifest update skipped: %v\n", err)
	}
	return nil
}

type runtimeManifest struct {
	CheckedAt         string                       `json:"checked_at"`
	DFGameR           runtimeFileStatus            `json:"df_game_r"`
	DFGameRBackup     runtimeFileStatus            `json:"df_game_r_backup"`
	ConfigFiles       map[string]runtimeFileStatus `json:"config_files"`
	GameFiles         map[string]runtimeFileStatus `json:"game_files"`
	AllRuntimeFilesOK bool                         `json:"all_runtime_files_ok"`
	ExpectedGameDir   string                       `json:"expected_game_dir"`
}

type runtimeFileStatus struct {
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	Size         int64  `json:"size,omitempty"`
	ModTime      int64  `json:"mod_time,omitempty"`
	MD5          string `json:"md5,omitempty"`
	SameAsConfig bool   `json:"same_as_config,omitempty"`
}

func ensureConfigRuntimeFiles(configDir string) error {
	if err := releaseDefaults(configDir); err != nil {
		return err
	}
	normalizeConfigFileModes(configDir)
	return nil
}

func releaseDefaults(configDir string) error {
	return fs.WalkDir(defaultFiles, "defaults", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := defaultFiles.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path, "defaults/")
		dst := filepath.Join(configDir, name)
		if _, err := os.Stat(dst); err == nil {
			if name == "robot_shout_templates.json" {
				current, readErr := os.ReadFile(dst)
				if readErr == nil && bytes.Contains(current, []byte(`"hello"`)) && bytes.Contains(current, []byte(`"team up"`)) {
					return os.WriteFile(dst, data, 0644)
				}
			}
			return nil
		}
		return os.WriteFile(dst, data, 0644)
	})
}

func normalizeConfigFileModes(configDir string) {
	_ = os.Chmod(filepath.Join(configDir, "privatekey.pem"), 0600)
	_ = os.Chmod(filepath.Join(configDir, "publickey.pem"), 0644)
}

func updateRuntimeManifest(cfg *config.SysConfig) error {
	if cfg == nil || cfg.ConfigDir == "" || cfg.DFGameR == "" {
		return nil
	}
	manifestPath := filepath.Join(cfg.ConfigDir, "pvf_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var manifest map[string]interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
	}
	if manifest == nil {
		manifest = map[string]interface{}{}
	}
	runtimeStatus := buildRuntimeManifest(cfg)
	manifest["runtime"] = &runtimeStatus
	return pvf.WriteJSON(manifestPath, manifest)
}

func buildRuntimeManifest(cfg *config.SysConfig) runtimeManifest {
	gameDir := filepath.Dir(cfg.DFGameR)
	configFiles := map[string]runtimeFileStatus{}
	gameFiles := map[string]runtimeFileStatus{}
	for _, name := range []string{"privatekey.pem", "publickey.pem"} {
		cfgPath := filepath.Join(cfg.ConfigDir, name)
		gamePath := filepath.Join(gameDir, name)
		cfgStatus := fileStatus(cfgPath)
		gameStatus := fileStatus(gamePath)
		if cfgStatus.Exists && gameStatus.Exists && cfgStatus.MD5 != "" && cfgStatus.MD5 == gameStatus.MD5 {
			gameStatus.SameAsConfig = true
		}
		configFiles[name] = cfgStatus
		gameFiles[name] = gameStatus
	}
	out := runtimeManifest{
		CheckedAt:         time.Now().Format(time.RFC3339),
		DFGameR:           fileStatus(cfg.DFGameR),
		DFGameRBackup:     fileStatus(cfg.DFGameR + ".tw_bak"),
		ConfigFiles:       configFiles,
		GameFiles:         gameFiles,
		ExpectedGameDir:   gameDir,
		AllRuntimeFilesOK: true,
	}
	for _, name := range []string{"privatekey.pem", "publickey.pem"} {
		if !configFiles[name].Exists || !gameFiles[name].Exists || !gameFiles[name].SameAsConfig {
			out.AllRuntimeFilesOK = false
		}
	}
	return out
}

func fileStatus(path string) runtimeFileStatus {
	out := runtimeFileStatus{Path: path}
	stat, err := os.Stat(path)
	if err != nil {
		return out
	}
	out.Exists = true
	out.Size = stat.Size()
	out.ModTime = stat.ModTime().Unix()
	if sum, err := fileMD5(path); err == nil {
		out.MD5 = sum
	}
	return out
}

func fileMD5(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:]), nil
}
