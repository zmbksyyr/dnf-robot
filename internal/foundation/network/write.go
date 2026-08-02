package network

import (
	"io"
)

// WriteFull writes the complete buffer or returns the first error. Writers
// are allowed to make partial progress, including without returning an error.
func WriteFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
