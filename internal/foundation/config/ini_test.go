package config

import (
	"os"
	"testing"
)

func TestINIValueKeepsCommentCharacters(t *testing.T) {
	cfg, err := LoadFromString("[db]\npassword = uu5!^%jg#semi;tail\n# ignored = yes\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.GetString("db", "password", "")
	want := "uu5!^%jg#semi;tail"
	if got != want {
		t.Fatalf("password mismatch: got %q want %q", got, want)
	}
}

func TestLoadConfigReadsPortsSection(t *testing.T) {
	path := t.TempDir() + "/config.ini"
	text := `[Ports]
RobotAPI = 18111
Web = 18112
Game = 20011
Monitor = 30304
Auction = 30804
Point = 30604
Relay = 7201
PartyRoute0 = 5064

[Robot]
DfGameR = /home/neople/game/df_game_r
ConfigDir = ./config
RobotInnerIp = 10.0.0.1
RobotConnectIp = 127.0.0.1

[Web]
WebPassword = twadmin
`
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RobotPort != 18111 || cfg.WebPort != 18112 || cfg.RobotGamePort != 20011 || cfg.MonitorPort != 30304 || cfg.AuctionPort != 30804 || cfg.PointPort != 30604 || cfg.RelayPort != 7201 || cfg.PartyRoute0Port != 5064 {
		t.Fatalf("ports not loaded: %+v", cfg)
	}
}
