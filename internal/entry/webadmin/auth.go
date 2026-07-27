package webadmin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.authed(r) {
		s.writeLogin(w, "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cleanIndexTemplate.Execute(w, map[string]interface{}{
		"RobotAddr": s.robotAddr,
		"WebAddr":   s.webAddr,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeLogin(w, "")
		return
	}
	_ = r.ParseForm()
	password := r.Form.Get("password")
	if strings.TrimSpace(s.cfg.WebPassword) == "" {
		s.writeLogin(w, "web password is not configured")
		return
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.WebPassword)) == 1 {
		token := randomToken()
		s.tokenMu.Lock()
		s.cleanupExpiredTokensLocked(time.Now())
		s.tokens[token] = time.Now().Add(12 * time.Hour)
		active := len(s.tokens)
		s.tokenMu.Unlock()
		fmt.Printf("[WebAdmin] session created pid=%d active=%d remote=%s\n", os.Getpid(), active, r.RemoteAddr)
		http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.writeLogin(w, "password error")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("tw_web_token"); err == nil {
		s.tokenMu.Lock()
		delete(s.tokens, c.Value)
		s.tokenMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "tw_web_token", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) writeLogin(w http.ResponseWriter, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cleanLoginTemplate.Execute(w, map[string]string{"Error": errText}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authed(r *http.Request) bool {
	if strings.TrimSpace(s.cfg.WebPassword) == "" {
		return false
	}
	c, err := r.Cookie("tw_web_token")
	if err != nil {
		return false
	}
	if c.Value == "" {
		fmt.Printf("[WebAdmin] auth rejected pid=%d reason=empty_token path=%s remote=%s\n", os.Getpid(), r.URL.Path, r.RemoteAddr)
		return false
	}
	now := time.Now()
	s.tokenMu.Lock()
	expires, ok := s.tokens[c.Value]
	if ok && now.After(expires) {
		delete(s.tokens, c.Value)
		ok = false
	}
	if ok {
		s.tokens[c.Value] = now.Add(12 * time.Hour)
	}
	s.cleanupExpiredTokensLocked(now)
	active := len(s.tokens)
	s.tokenMu.Unlock()
	if !ok {
		fmt.Printf("[WebAdmin] auth rejected pid=%d reason=unknown_or_expired_token active=%d path=%s remote=%s\n", os.Getpid(), active, r.URL.Path, r.RemoteAddr)
	}
	return ok
}

func (s *Server) sessionCount() int {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return len(s.tokens)
}

func (s *Server) cleanupExpiredTokensLocked(now time.Time) {
	for token, expires := range s.tokens {
		if now.After(expires) {
			delete(s.tokens, token)
		}
	}
}

func randomToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("webadmin random token: %v", err))
	}
	return hex.EncodeToString(raw[:])
}
