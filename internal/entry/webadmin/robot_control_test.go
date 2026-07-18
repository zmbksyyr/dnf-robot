package webadmin

import (
	"os"
	"path/filepath"
	"robot/internal/foundation/config"
	"strings"
	"testing"
)

func TestWriteGamePortUpdatesMainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	text := strings.Join([]string{
		"[Robot]",
		"robotPort = 8111",
		"RobotConnectIp = 127.0.0.1",
		"RobotGamePort = 10011",
		"",
		"[Web]",
		"WebPort = 8112",
		"WebPassword = twadmin",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	s := New(&config.SysConfig{ConfigDir: dir, RobotConnectIP: "127.0.0.1", RobotGamePort: 10011}, "", "")

	cfg, err := s.writeGamePort(20011)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RobotGamePort != 20011 || s.cfg.RobotGamePort != 20011 {
		t.Fatalf("game port was not updated: cfg=%d server=%d", cfg.RobotGamePort, s.cfg.RobotGamePort)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "RobotGamePort = 20011") {
		t.Fatalf("config file was not updated:\n%s", data)
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("/root/robot's/bin")
	want := "'/root/robot'\"'\"'s/bin'"
	if got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
}
