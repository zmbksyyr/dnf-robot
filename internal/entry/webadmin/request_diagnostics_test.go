package webadmin

import (
	"errors"
	"net/http"
	"testing"
)

type failingResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (*failingResponseWriter) WriteHeader(int) {}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestStatusRecorderCapturesResponseWriteError(t *testing.T) {
	want := errors.New("client disconnected")
	recorder := &statusRecorder{ResponseWriter: &failingResponseWriter{err: want}}
	if _, err := recorder.Write([]byte("response")); !errors.Is(err, want) {
		t.Fatalf("write error = %v, want %v", err, want)
	}
	if !errors.Is(recorder.writeErr, want) {
		t.Fatalf("recorded error = %v, want %v", recorder.writeErr, want)
	}
}
