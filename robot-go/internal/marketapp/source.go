package marketapp

import (
	"encoding/json"
	"fmt"
	"os"
)

func (a *App) loadRestockSeed() (restockSeed, error) {
	path := restockFilePath(a.configDir, a.cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		return restockSeed{}, fmt.Errorf("read restock file %s: %w", path, err)
	}
	var seed restockSeed
	if err := json.Unmarshal(data, &seed); err != nil {
		return restockSeed{}, fmt.Errorf("parse restock file %s: %w", path, err)
	}
	return seed, nil
}
