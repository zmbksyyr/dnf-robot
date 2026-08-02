package webadmin

import (
	"errors"
	"fmt"
	"io"
)

type memoryReadWriter interface {
	io.ReaderAt
	io.WriterAt
}

type memoryRestore struct {
	address int64
	value   []byte
}

func restoreMemoryVerified(mem memoryReadWriter, restores ...memoryRestore) error {
	var errs []error
	for _, restore := range restores {
		if err := writeMemoryVerified(mem, restore.address, restore.value); err != nil {
			errs = append(errs, fmt.Errorf("0x%x: %w", restore.address, err))
		}
	}
	return errors.Join(errs...)
}

func memoryPatchError(operation string, err, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%s: %w; rollback failed: %v", operation, err, rollbackErr)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
