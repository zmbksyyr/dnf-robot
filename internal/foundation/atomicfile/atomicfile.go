// Package atomicfile publishes complete files without exposing partially
// written content to concurrent readers.
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile atomically replaces path with data. The temporary file is created
// beside path so the final rename stays on the same filesystem.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	tempPath, err := writeTemp(path, data, perm)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	return os.Rename(tempPath, path)
}

// WriteFileIfMissing atomically creates path without replacing an existing
// file. It returns true only when this call published the file.
func WriteFileIfMissing(path string, data []byte, perm fs.FileMode) (bool, error) {
	tempPath, err := writeTemp(path, data, perm)
	if err != nil {
		return false, err
	}
	defer os.Remove(tempPath)

	// Linking a complete same-directory temporary file gives create-if-absent
	// semantics without a stat/write race or a partially visible destination.
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func writeTemp(path string, data []byte, perm fs.FileMode) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(perm); err != nil {
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	keep = true
	return tempPath, nil
}
