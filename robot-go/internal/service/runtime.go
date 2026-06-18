package service

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

	"robot/internal/config"
	"robot/internal/dnf"
)

//go:embed defaults/*
var defaultFiles embed.FS

//go:embed defaults/libantisvrinline.so
var inlineSO []byte

func InitRuntime(cfg *config.SysConfig) error {
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
	if err := ensureGameRuntimeFiles(cfg); err != nil {
		return err
	}
	EnsureRuntimeKeypair(cfg)
	if err := ensurePVFExports(cfg.DFGameR, cfg.ConfigDir); err != nil {
		return err
	}
	if err := ensureDFGameRPatches(cfg.DFGameR); err != nil {
		return err
	}
	if err := updateRuntimeManifest(cfg); err != nil {
		dnf.PrintfBlue("[Runtime] self-check manifest update skipped: %v\n", err)
	}
	return nil
}

type runtimeManifest struct {
	CheckedAt         string                       `json:"checked_at"`
	DFGameR           runtimeFileStatus            `json:"df_game_r"`
	DFGameRBackup     runtimeFileStatus            `json:"df_game_r_backup"`
	ConfigFiles       map[string]runtimeFileStatus `json:"config_files"`
	GameFiles         map[string]runtimeFileStatus `json:"game_files"`
	Patch             runtimePatchStatus           `json:"patch"`
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

type runtimePatchStatus struct {
	AntiImportRedirect bool `json:"anti_import_redirect"`
	OriginalImportName bool `json:"original_import_name"`
	BackupExists       bool `json:"backup_exists"`
	Patched            bool `json:"patched"`
}

func ensureConfigRuntimeFiles(configDir string) error {
	if err := releaseDefaults(configDir); err != nil {
		return err
	}
	if err := releaseInlineSOToConfig(configDir); err != nil {
		return err
	}
	normalizeConfigFileModes(configDir)
	return nil
}

func ensureGameRuntimeFiles(cfg *config.SysConfig) error {
	return deployRuntimeFilesToGame(cfg)
}

func ensureDFGameRPatches(dfGameR string) error {
	if dfGameR == "" {
		return nil
	}
	status := detectRuntimePatch(dfGameR)
	if status.Patched {
		dnf.PrintfGreen("[Runtime] df_game_r patch self-check ok - skipping patch.\n")
		return nil
	}
	if !status.AntiImportRedirect {
		dnf.PrintfBlue("[Runtime] applying anti import redirect patch.\n")
		if err := dnf.RefishDfGameR(dfGameR); err != nil {
			return err
		}
	}
	status = detectRuntimePatch(dfGameR)
	if !status.Patched {
		return fmt.Errorf("df_game_r anti import redirect patch incomplete: anti_import=%t", status.AntiImportRedirect)
	}
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

func releaseInlineSOToConfig(configDir string) error {
	if configDir == "" {
		return nil
	}
	dst := filepath.Join(configDir, "libantisvrinline.so")
	if _, err := os.Stat(dst); err == nil {
		current, readErr := os.ReadFile(dst)
		if readErr == nil && !bytes.Equal(current, inlineSO) {
			if err := backupOnce(dst); err != nil {
				return err
			}
			if err := os.WriteFile(dst, inlineSO, 0755); err != nil {
				return err
			}
			dnf.PrintfGreen("[Runtime] updated libantisvrinline.so in %s\n", configDir)
		}
		_ = os.Chmod(dst, 0755)
		return nil
	}
	if len(inlineSO) == 0 {
		return fmt.Errorf("embedded libantisvrinline.so is empty")
	}
	if err := os.WriteFile(dst, inlineSO, 0755); err != nil {
		return err
	}
	_ = os.Chmod(dst, 0755)
	dnf.PrintfGreen("[Runtime] released libantisvrinline.so to %s\n", dst)
	return nil
}

func normalizeConfigFileModes(configDir string) {
	_ = os.Chmod(filepath.Join(configDir, "libantisvrinline.so"), 0755)
	_ = os.Chmod(filepath.Join(configDir, "privatekey.pem"), 0600)
	_ = os.Chmod(filepath.Join(configDir, "publickey.pem"), 0644)
}

func deployRuntimeFilesToGame(cfg *config.SysConfig) error {
	if cfg.DFGameR == "" {
		return nil
	}
	gameDir := filepath.Dir(cfg.DFGameR)
	files := []struct {
		name string
		mode os.FileMode
	}{
		{name: "libantisvrinline.so", mode: 0755},
	}
	for _, file := range files {
		src := filepath.Join(cfg.ConfigDir, file.name)
		dst := filepath.Join(gameDir, file.name)
		if err := syncRuntimeFile(src, dst, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func syncRuntimeFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(current, data) {
			_ = os.Chmod(dst, mode)
			dnf.PrintfGreen("[Runtime] %s already current - skipping copy.\n", filepath.Base(dst))
			return nil
		}
		if err := backupOnce(dst); err != nil {
			return err
		}
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	_ = os.Chmod(dst, mode)
	dnf.PrintfGreen("[Runtime] copied %s to %s\n", filepath.Base(src), dst)
	return nil
}

func backupOnce(path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	backupPath := path + ".tw_bak"
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return err
	}
	dnf.PrintfBlue("[Runtime] backup created: %s\n", backupPath)
	return nil
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
	var manifest pvfManifest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
	}
	runtimeStatus := buildRuntimeManifest(cfg)
	manifest.Runtime = &runtimeStatus
	return writeJSON(manifestPath, manifest)
}

func buildRuntimeManifest(cfg *config.SysConfig) runtimeManifest {
	gameDir := filepath.Dir(cfg.DFGameR)
	configFiles := map[string]runtimeFileStatus{}
	gameFiles := map[string]runtimeFileStatus{}
	for _, name := range []string{"libantisvrinline.so", "privatekey.pem", "publickey.pem"} {
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
	patch := detectRuntimePatch(cfg.DFGameR)
	out := runtimeManifest{
		CheckedAt:         time.Now().Format(time.RFC3339),
		DFGameR:           fileStatus(cfg.DFGameR),
		DFGameRBackup:     fileStatus(cfg.DFGameR + ".tw_bak"),
		ConfigFiles:       configFiles,
		GameFiles:         gameFiles,
		Patch:             patch,
		ExpectedGameDir:   gameDir,
		AllRuntimeFilesOK: patch.Patched,
	}
	for _, name := range []string{"libantisvrinline.so", "privatekey.pem", "publickey.pem"} {
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

func detectRuntimePatch(dfGameR string) runtimePatchStatus {
	data, err := os.ReadFile(dfGameR)
	if err != nil {
		return runtimePatchStatus{}
	}
	hasInline := bytes.Contains(data, []byte("libantisvrinline.so"))
	hasOriginal := bytes.Contains(data, []byte("libantisvrimport.so"))
	status := runtimePatchStatus{
		AntiImportRedirect: hasInline,
		OriginalImportName: hasOriginal,
		BackupExists:       fileExists(dfGameR + ".tw_bak"),
	}
	status.Patched = hasInline && !hasOriginal
	return status
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
