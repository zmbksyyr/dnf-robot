//go:build windows

package process

import (
	"fmt"
	"io"
	"syscall"
	"unsafe"
)

type MemoryFile interface {
	io.ReaderAt
	io.WriterAt
}
type windowsMemory struct {
	handle   syscall.Handle
	writable bool
}

var readMemory = kernel32.NewProc("ReadProcessMemory")
var writeMemory = kernel32.NewProc("WriteProcessMemory")
var protectMemory = kernel32.NewProc("VirtualProtectEx")
var flushInstructions = kernel32.NewProc("FlushInstructionCache")

func (m windowsMemory) ReadAt(data []byte, off int64) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	var size uintptr
	ok, _, err := readMemory.Call(uintptr(m.handle), uintptr(off), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return int(size), fmt.Errorf("read process memory at 0x%x: %w", off, err)
	}
	if int(size) != len(data) {
		return int(size), io.ErrUnexpectedEOF
	}
	return int(size), nil
}
func (m windowsMemory) WriteAt(data []byte, off int64) (int, error) {
	if !m.writable {
		return 0, fmt.Errorf("process memory is read-only")
	}
	if len(data) == 0 {
		return 0, nil
	}
	var old uint32
	ok, _, err := protectMemory.Call(uintptr(m.handle), uintptr(off), uintptr(len(data)), 0x40, uintptr(unsafe.Pointer(&old)))
	if ok == 0 {
		return 0, fmt.Errorf("protect process memory: %w", err)
	}
	var size uintptr
	ok, _, writeErr := writeMemory.Call(uintptr(m.handle), uintptr(off), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&size)))
	var ignored uint32
	restored, _, restoreErr := protectMemory.Call(uintptr(m.handle), uintptr(off), uintptr(len(data)), uintptr(old), uintptr(unsafe.Pointer(&ignored)))
	flushed, _, flushErr := flushInstructions.Call(uintptr(m.handle), uintptr(off), uintptr(len(data)))
	if ok == 0 {
		return int(size), fmt.Errorf("write process memory: %w", writeErr)
	}
	if restored == 0 {
		return int(size), fmt.Errorf("restore protection: %w", restoreErr)
	}
	if flushed == 0 {
		return int(size), fmt.Errorf("flush instructions: %w", flushErr)
	}
	if int(size) != len(data) {
		return int(size), io.ErrShortWrite
	}
	return int(size), nil
}
func WithMemoryFile(pid int, writable bool, probeAddress int64, fn func(MemoryFile, bool) error) error {
	rights := uint32(0x10)
	if writable {
		rights |= 0x20 | 0x8
	}
	h, err := syscall.OpenProcess(rights, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	mem := windowsMemory{h, writable}
	var probe [1]byte
	if _, err = mem.ReadAt(probe[:], probeAddress); err != nil {
		return err
	}
	return fn(mem, false)
}
