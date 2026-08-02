package webadmin

import (
	"fmt"
	"testing"
	"time"
)

func TestStoreSessionTokenKeepsBoundedRecentSessions(t *testing.T) {
	server := &Server{tokens: make(map[string]time.Time)}
	base := time.Now()
	for i := 0; i < maxWebSessions; i++ {
		server.storeSessionTokenLocked(fmt.Sprintf("token-%d", i), base.Add(time.Duration(i)*time.Minute))
	}
	server.storeSessionTokenLocked("new-token", base.Add(24*time.Hour))

	if got := len(server.tokens); got != maxWebSessions {
		t.Fatalf("session count got %d want %d", got, maxWebSessions)
	}
	if _, ok := server.tokens["token-0"]; ok {
		t.Fatal("oldest session was not evicted")
	}
	if _, ok := server.tokens["new-token"]; !ok {
		t.Fatal("new session was not retained")
	}
}
