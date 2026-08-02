// Package filebackup stores bounded recovery copies before Robot patches an
// external file.
package filebackup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Save publishes data as path and keeps at most count copies. path is the
// newest copy; numeric suffixes contain progressively older copies.
func Save(path string, data []byte, mode fs.FileMode, count int) error {
	if path == "" {
		return fmt.Errorf("empty backup path")
	}
	if count <= 0 {
		return fmt.Errorf("invalid backup count %d", count)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if count == 1 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		oldest := numberedPath(path, count-1)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return err
		}
		for index := count - 2; index >= 1; index-- {
			if err := renameIfPresent(numberedPath(path, index), numberedPath(path, index+1)); err != nil {
				return err
			}
		}
		if err := renameIfPresent(path, numberedPath(path, 1)); err != nil {
			return err
		}
	}
	return os.Rename(tempPath, path)
}

func numberedPath(path string, index int) string {
	return fmt.Sprintf("%s.%d", path, index)
}

func renameIfPresent(from, to string) error {
	if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
