package network

import (
	"bytes"
	"io"
	"testing"
)

type chunkWriter struct {
	buffer bytes.Buffer
	max    int
}

func (w *chunkWriter) Write(data []byte) (int, error) {
	if len(data) > w.max {
		data = data[:w.max]
	}
	return w.buffer.Write(data)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestWriteFullHandlesPartialWrites(t *testing.T) {
	writer := &chunkWriter{max: 2}
	if err := WriteFull(writer, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := writer.buffer.String(); got != "abcdef" {
		t.Fatalf("written data = %q", got)
	}
}

func TestWriteFullRejectsNoProgress(t *testing.T) {
	if err := WriteFull(zeroWriter{}, []byte("x")); err != io.ErrShortWrite {
		t.Fatalf("zero write error = %v, want %v", err, io.ErrShortWrite)
	}
}
