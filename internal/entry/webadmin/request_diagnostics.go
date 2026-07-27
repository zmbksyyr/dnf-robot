package webadmin

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (s *Server) withDiagnostics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			duration := time.Since(start)
			if v := recover(); v != nil {
				fmt.Printf("[WebAdmin] panic pid=%d method=%s path=%s remote=%s duration=%s err=%v\n%s\n", os.Getpid(), r.Method, r.URL.Path, r.RemoteAddr, duration.Round(time.Millisecond), v, debug.Stack())
				http.Error(rec, "internal server error", http.StatusInternalServerError)
				return
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 500 || duration > 3*time.Second {
				fmt.Printf("[WebAdmin] request pid=%d method=%s path=%s status=%d bytes=%d duration=%s remote=%s\n", os.Getpid(), r.Method, r.URL.Path, status, rec.bytes, duration.Round(time.Millisecond), r.RemoteAddr)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
