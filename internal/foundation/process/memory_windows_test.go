//go:build windows

package process

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
)

func TestWindowsMemoryReadWriteAndReadOnly(t *testing.T) {
	data := []byte{0x46, 0x07}
	address := int64(uintptr(unsafe.Pointer(&data[0])))
	err := WithMemoryFile(os.Getpid(), false, address, func(memory MemoryFile, _ bool) error {
		var read [2]byte
		if _, err := memory.ReadAt(read[:], address); err != nil {
			return err
		}
		if read != [2]byte{0x46, 0x07} {
			t.Fatalf("read=%x", read)
		}
		if _, err := memory.WriteAt([]byte{0x7f}, address); err == nil {
			t.Fatal("read-only memory allowed a write")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = WithMemoryFile(os.Getpid(), true, address, func(memory MemoryFile, _ bool) error {
		_, err := memory.WriteAt([]byte{0x7f}, address)
		return err
	})
	if err != nil || data[0] != 0x7f || data[1] != 0x07 {
		t.Fatalf("data=%x error=%v", data, err)
	}
	runtime.KeepAlive(data)
}
