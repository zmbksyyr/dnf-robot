package webadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"robot/internal/foundation/config"
)

func TestSecurityHeaders(t *testing.T) {
	s := New(&config.SysConfig{WebPassword: "secret"}, "", "")
	h := s.withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, name := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Cache-Control"} {
		if rec.Header().Get(name) == "" {
			t.Fatalf("missing security header %s", name)
		}
	}
}

func TestLoginFailureRateLimit(t *testing.T) {
	s := New(&config.SysConfig{WebPassword: "secret"}, "", "")
	form := url.Values{"password": {"wrong"}}.Encode()
	for i := 0; i < maxLoginFailures; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		s.handleLogin(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d blocked before threshold", i+1)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.10:4321"
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limited response code=%d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}
