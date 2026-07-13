package tcpapi

import (
	robotcap "robot/internal/capability/robot"
	"testing"
)

func TestRequiresValidKeypair(t *testing.T) {
	if !RequiresValidKeypair("robotsOnline") {
		t.Fatal("robotsOnline should require a valid keypair")
	}
	if RequiresValidKeypair("sys") {
		t.Fatal("sys should not require a valid keypair")
	}
}

func TestExtractTagContent(t *testing.T) {
	pkt := `<tw><c>sys</c><json>{"ok":true}</json></tw>`
	if got := extractTagContent(pkt, "c"); got != "sys" {
		t.Fatalf("command=%q, want sys", got)
	}
	if got := extractPayload(pkt); got != `{"ok":true}` {
		t.Fatalf("payload=%q", got)
	}
}

func TestCleanupRequiresAsync(t *testing.T) {
	if !cleanupRequiresAsync(robotcap.CleanupRequest{Force: true}) {
		t.Fatal("forced full cleanup should require async")
	}
	if cleanupRequiresAsync(robotcap.CleanupRequest{Force: false}) {
		t.Fatal("dry-run full cleanup should remain synchronous")
	}
	if cleanupRequiresAsync(robotcap.CleanupRequest{Force: true, UIDs: []int{1}}) {
		t.Fatal("scoped uid cleanup should remain synchronous")
	}
	if cleanupRequiresAsync(robotcap.CleanupRequest{Force: true, MinUID: 1, MaxUID: 10}) {
		t.Fatal("scoped range cleanup should remain synchronous")
	}
}
