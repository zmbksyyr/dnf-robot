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
		"[Ports]",
		"RobotAPI = 8111",
		"Web = 8112",
		"Game = 10011",
		"Monitor = 30303",
		"Auction = 30803",
		"Point = 30603",
		"Relay = 7200",
		"PartyRoute0 = 5063",
		"",
		"[Robot]",
		"RobotConnectIp = 127.0.0.1",
		"",
		"[Web]",
		"WebPassword = twadmin",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	s := New(&config.SysConfig{ConfigDir: dir, RobotConnectIP: "127.0.0.1", RobotGamePort: 10011, MonitorPort: 30303, AuctionPort: 30803, PointPort: 30603, RelayPort: 7200}, "", "")

	cfg, err := s.writeExternalPorts(20011, 31303, 31803, 31603, 17200)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RobotGamePort != 20011 || cfg.MonitorPort != 31303 || cfg.AuctionPort != 31803 || cfg.PointPort != 31603 || cfg.RelayPort != 17200 {
		t.Fatalf("ports were not updated: cfg=%+v", cfg)
	}
	if s.cfg.RobotGamePort != 20011 || s.cfg.MonitorPort != 31303 || s.cfg.AuctionPort != 31803 || s.cfg.PointPort != 31603 || s.cfg.RelayPort != 17200 {
		t.Fatalf("server ports were not updated: cfg=%+v", s.cfg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Game = 20011", "Monitor = 31303", "Auction = 31803", "Point = 31603", "Relay = 17200"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config file missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "RobotGamePort") {
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
