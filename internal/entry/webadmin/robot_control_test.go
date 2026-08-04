package webadmin

import (
	"os"
	"path/filepath"
	"robot/internal/foundation/config"
	"robot/internal/foundation/layout"
	"strings"
	"testing"
)

func TestBuildRobotRestartScriptKeepsOtherBoundedLogSinks(t *testing.T) {
	script := buildRobotRestartScript("/root/robot", "/root/config")
	for _, want := range []string{
		`[ "$mode" = "--web-admin" ]`,
		`[ "$mode" = "--bounded-log-sink" ] && [ "$sink" = "$log_path" ]`,
		"log_path=" + shellQuote(filepath.Join("/root/config", "logs", "stdout.log")),
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("restart script missing %q:\n%s", want, script)
		}
	}
}

func TestConfigPathRejectsMissingRuntimeRoot(t *testing.T) {
	if got := (&Server{}).configPath(); got != "" {
		t.Fatalf("config path = %q, want empty path without configured runtime root", got)
	}
}

func TestWriteGamePortUpdatesMainConfig(t *testing.T) {
	dir := t.TempDir()
	paths := layout.New(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	path := paths.MainConfig()
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

	cfg, err := s.writeExternalServices(20011, 31303, 31803, 31603, 17200, "127.0.0.1", "127.0.0.1", "127.0.0.1", "/home/neople", "/root/run", "auto", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RobotGamePort != 20011 || cfg.MonitorPort != 31303 || cfg.AuctionPort != 31803 || cfg.PointPort != 31603 || cfg.RelayPort != 17200 {
		t.Fatalf("ports were not updated: cfg=%+v", cfg)
	}
	if s.cfg.RobotGamePort != 10011 || s.cfg.MonitorPort != 30303 || s.cfg.AuctionPort != 30803 || s.cfg.PointPort != 30603 || s.cfg.RelayPort != 7200 {
		t.Fatalf("running server ports changed before restart: cfg=%+v", s.cfg)
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
