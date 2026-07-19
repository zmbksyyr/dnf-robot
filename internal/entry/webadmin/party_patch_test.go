package webadmin

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"robot/internal/foundation/config"
)

func testPartyCompatLayout() partyCompatLayout {
	return partyCompatLayout{site: 64, cave: 128, rawSend: 320, resumeSite: 74, getPacket: 256}
}

func TestPartyCompatDefaultRangeCoversFirstThousandRobotAccounts(t *testing.T) {
	if defaultPartyCompatAccountStart != 17000000 || defaultPartyCompatAccountEnd != 17001000 {
		t.Fatalf("default range = %d..%d", defaultPartyCompatAccountStart, defaultPartyCompatAccountEnd)
	}
}

func TestPartyCompatConfigDefaultsDesiredOn(t *testing.T) {
	dir := t.TempDir()
	s := New(&config.SysConfig{ConfigDir: dir}, "", "")
	cfg, err := s.loadPartyCompatConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.AccountStart != defaultPartyCompatAccountStart || cfg.AccountEnd != defaultPartyCompatAccountEnd {
		t.Fatalf("default config = %+v", cfg)
	}

	if err := os.WriteFile(s.partyCompatConfigPath(), []byte(`{"account_start":17000000,"account_end":17001000}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.loadPartyCompatConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatalf("legacy config without enabled should default on: %+v", cfg)
	}
}

func TestPartyCompatRetryBackoffAndAutoOffThreshold(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, delay := range want {
		if got := partyCompatRetryDelay(i + 1); got != delay {
			t.Fatalf("delay[%d] = %s, want %s", i, got, delay)
		}
	}

	s := New(&config.SysConfig{ConfigDir: t.TempDir()}, "", "")
	now := time.Now()
	s.partyCompatFailures = partyCompatDisableAfterFailures - 1
	s.partyCompatFirstFailure = now.Add(-partyCompatDisableAfter)
	if s.partyCompatShouldDisableLocked(now) {
		t.Fatal("disabled before failure threshold")
	}
	s.partyCompatFailures = partyCompatDisableAfterFailures
	s.partyCompatFirstFailure = now.Add(-partyCompatDisableAfter + time.Second)
	if s.partyCompatShouldDisableLocked(now) {
		t.Fatal("disabled before age threshold")
	}
	s.partyCompatFirstFailure = now.Add(-partyCompatDisableAfter)
	if !s.partyCompatShouldDisableLocked(now) {
		t.Fatal("did not disable after count and age thresholds")
	}
}

func TestPartyCompatUnavailableRetryDoesNotAccumulateAutoOffFailures(t *testing.T) {
	s := New(&config.SysConfig{ConfigDir: t.TempDir()}, "", "")
	now := time.Unix(1000, 0)
	s.partyCompatFailures = partyCompatDisableAfterFailures
	s.partyCompatFirstFailure = now.Add(-partyCompatDisableAfter)
	s.partyCompatLastError = "old patch failure"

	delay := s.schedulePartyCompatUnavailableRetryLocked("df_game_r is not listening on port 10011", now)
	if delay != partyCompatInitialRetry {
		t.Fatalf("retry delay = %s, want %s", delay, partyCompatInitialRetry)
	}
	if s.partyCompatFailures != 0 || !s.partyCompatFirstFailure.IsZero() || s.partyCompatShouldDisableLocked(now) {
		t.Fatalf("unavailable process retained auto-off failures: count=%d first=%s", s.partyCompatFailures, s.partyCompatFirstFailure)
	}
	if want := now.Add(partyCompatInitialRetry); !s.partyCompatNextRetry.Equal(want) {
		t.Fatalf("next retry = %s, want %s", s.partyCompatNextRetry, want)
	}
	if want := "waiting for df_game_r: df_game_r is not listening on port 10011"; s.partyCompatLastError != want {
		t.Fatalf("waiting message = %q, want %q", s.partyCompatLastError, want)
	}
}

func newPartyCompatMemory(t *testing.T, layout partyCompatLayout) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "party-compat-mem")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(512); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(partyCompatOriginalSite, layout.site); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestBuildPartyCompatCaveUsesCUserArgumentAndRange(t *testing.T) {
	layout := defaultPartyCompatLayout
	site, err := buildPartyCompatSite(layout)
	if err != nil {
		t.Fatal(err)
	}
	wantSite, _ := hex.DecodeString("e9c5f44a009090909090")
	if !bytes.Equal(site, wantSite) {
		t.Fatalf("site = %x, want %x", site, wantSite)
	}
	cave, err := buildPartyCompatCave(layout, 17000000, 18000000)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix, _ := hex.DecodeString("8b45088b80ac0407003d406603010f82540000003d80a812010f8349000000c7442404000000008b450c890424e8ec4073ff0fb740016683f8060f82280000006683f80b0f862d0000006683f8160f82140000006683f81f0f8619000000663d99000f840f000000807dd3000f848d0db5ffe9c90ab5ffe9830db5ff")
	if len(cave) != len(partyCompatZeroCave) || !bytes.Equal(cave[:len(wantPrefix)], wantPrefix) || !allZero(cave[len(wantPrefix):]) {
		t.Fatalf("cave = %x, want prefix %x and zero padding", cave, wantPrefix)
	}
	start, end, ok := parsePartyCompatCave(layout, cave)
	if !ok || start != 17000000 || end != 18000000 {
		t.Fatalf("parsed cave = %d..%d ok=%t", start, end, ok)
	}
}

func TestSetPartyCompatMemoryOnUpdateOff(t *testing.T) {
	layout := testPartyCompatLayout()
	mem := newPartyCompatMemory(t, layout)

	changed, err := setPartyCompatMemory(mem, layout, 17000000, 18000000, true)
	if err != nil || !changed {
		t.Fatalf("enable changed=%t err=%v", changed, err)
	}
	enabled, start, end, err := inspectPartyCompatMemory(mem, layout)
	if err != nil || !enabled || start != 17000000 || end != 18000000 {
		t.Fatalf("enabled=%t range=%d..%d err=%v", enabled, start, end, err)
	}

	changed, err = setPartyCompatMemory(mem, layout, 17001000, 17002000, true)
	if err != nil || !changed {
		t.Fatalf("update changed=%t err=%v", changed, err)
	}
	enabled, start, end, err = inspectPartyCompatMemory(mem, layout)
	if err != nil || !enabled || start != 17001000 || end != 17002000 {
		t.Fatalf("updated=%t range=%d..%d err=%v", enabled, start, end, err)
	}

	changed, err = setPartyCompatMemory(mem, layout, 17001000, 17002000, false)
	if err != nil || !changed {
		t.Fatalf("disable changed=%t err=%v", changed, err)
	}
	enabled, _, _, err = inspectPartyCompatMemory(mem, layout)
	if err != nil || enabled {
		t.Fatalf("enabled=%t err=%v", enabled, err)
	}
}

func TestSetPartyCompatMemoryRejectsOccupiedCave(t *testing.T) {
	layout := testPartyCompatLayout()
	mem := newPartyCompatMemory(t, layout)
	if _, err := mem.WriteAt([]byte{1}, layout.cave); err != nil {
		t.Fatal(err)
	}
	if _, err := setPartyCompatMemory(mem, layout, 17000000, 18000000, true); err == nil {
		t.Fatal("occupied cave was accepted")
	}
}

func TestParseGamePIDForPort(t *testing.T) {
	data := []byte(`LISTEN 0 128 *:10011 *:* users:(("df_game_r",pid=499,fd=27))
LISTEN 0 128 :::8111 :::* users:(("robot",pid=12,fd=8))`)
	pid, err := parseGamePIDForPort(data, 10011)
	if err != nil || pid != 499 {
		t.Fatalf("pid=%d err=%v", pid, err)
	}
	if _, err := parseGamePIDForPort(data, 10056); !errors.Is(err, errPartyCompatUnavailable) {
		t.Fatalf("missing game port error = %v", err)
	}
}
