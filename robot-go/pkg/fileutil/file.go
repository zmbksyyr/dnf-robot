package fileutil

import "os"

func ReadFileToBuf(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
