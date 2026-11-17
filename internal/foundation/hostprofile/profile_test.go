package hostprofile

import (
	"path/filepath"
	"testing"

	"robot/internal/foundation/config"
)

func TestLoadOrDetectPersistsProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "host-profile.json")
	cfg := &config.SysConfig{ServiceRoot: `C:\instance\home\neople`}
	first, created, err := LoadOrDetect(path, cfg)
	if err != nil || !created {
		t.Fatalf("create profile: created=%v err=%v", created, err)
	}
	second, created, err := LoadOrDetect(path, nil)
	if err != nil || created {
		t.Fatalf("reload profile: created=%v err=%v", created, err)
	}
	if first.Adapter != second.Adapter || second.ServiceRoot != cfg.ServiceRoot || second.MarketOwner != "robot" {
		t.Fatalf("profile changed across reload: first=%+v second=%+v", first, second)
	}
}

func TestValidateRejectsUnknownMarketOwner(t *testing.T) {
	p := Default(nil)
	p.MarketOwner = "other"
	if err := Validate(p); err == nil {
		t.Fatal("unknown market owner accepted")
	}
}

func TestValidateAllowsCommandLineIdentity(t *testing.T) {
	p := Default(nil)
	p.Services["game"] = Service{CommandLineContains: "df_game_r"}
	if err := Validate(p); err != nil {
		t.Fatalf("command-line service identity rejected: %v", err)
	}
}
