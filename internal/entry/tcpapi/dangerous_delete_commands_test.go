package tcpapi

import (
	"strings"
	"testing"
	"time"
)

func TestDangerousDeleteCommandsRejectNonLoopbackClients(t *testing.T) {
	for _, cmd := range []string{"dangerousDeleteUnlock", "dangerousDeleteAsync"} {
		response, handled := handleDangerousDeleteCommand("192.168.200.10:12345", cmd, "", nil)
		if !handled {
			t.Fatalf("%s was not handled", cmd)
		}
		if !strings.Contains(response, "only available through the local web admin") {
			t.Fatalf("%s response=%q", cmd, response)
		}
	}
}

func TestDangerousDeleteTokenIsSingleUse(t *testing.T) {
	token, err := issueDangerousDeleteToken("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !consumeDangerousDeleteToken(token, "127.0.0.1", time.Now()) {
		t.Fatal("fresh token should be accepted")
	}
	if consumeDangerousDeleteToken(token, "127.0.0.1", time.Now()) {
		t.Fatal("token must be single use")
	}
}

func TestDangerousDeleteTokenRejectsExpiredValue(t *testing.T) {
	token := "expired-test-token"
	dangerousDeleteTokens.Store(token, dangerousDeleteToken{Expires: time.Now().Add(-time.Second), ClientIP: "127.0.0.1"})
	if consumeDangerousDeleteToken(token, "127.0.0.1", time.Now()) {
		t.Fatal("expired token should be rejected")
	}
}

func TestDangerousDeleteTokenIsBoundToClientIP(t *testing.T) {
	token, err := issueDangerousDeleteToken("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if consumeDangerousDeleteToken(token, "::1", time.Now()) {
		t.Fatal("token must not be accepted from another client IP")
	}
}

func TestLoopbackClientIP(t *testing.T) {
	tests := []struct {
		clientID string
		wantIP   string
		wantOK   bool
	}{
		{clientID: "127.0.0.1:12345", wantIP: "127.0.0.1", wantOK: true},
		{clientID: "[::1]:12345", wantIP: "::1", wantOK: true},
		{clientID: "192.168.200.10:12345", wantOK: false},
		{clientID: "invalid", wantOK: false},
	}
	for _, tt := range tests {
		gotIP, gotOK := loopbackClientIP(tt.clientID)
		if gotIP != tt.wantIP || gotOK != tt.wantOK {
			t.Fatalf("loopbackClientIP(%q) = (%q, %v), want (%q, %v)", tt.clientID, gotIP, gotOK, tt.wantIP, tt.wantOK)
		}
	}
}
