package tcpapi

import "testing"

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
