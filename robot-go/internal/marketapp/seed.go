package marketapp

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed seeds/market_restock_seed.json
var seedFiles embed.FS

func ensureRestockFile(configDir string, cfg Config) error {
	path := restockFilePath(configDir, cfg)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := seedFiles.ReadFile("seeds/market_restock_seed.json")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func restockFilePath(configDir string, cfg Config) string {
	name := cfg.Restock.File
	if name == "" {
		name = DefaultConfig().Restock.File
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(configDir, name)
}
