package network

import "testing"

func TestParseURL(t *testing.T) {
	host, file, port, err := ParseURL("https://example.com:8443/path/file?q=1")
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || file != "path/file?q=1" || port != 8443 {
		t.Fatalf("unexpected parse: host=%q file=%q port=%d", host, file, port)
	}
}

func TestParseURLInvalidPort(t *testing.T) {
	if _, _, _, err := ParseURL("example.com:notaport/path"); err == nil {
		t.Fatal("expected invalid port error")
	}
}
