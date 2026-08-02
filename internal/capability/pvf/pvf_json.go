package pvf

import (
	"encoding/json"
	"io/fs"
	"os"

	"robot/internal/foundation/atomicfile"
)

func WriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	return atomicfile.WriteFile(path, data, fs.FileMode(mode))
}
