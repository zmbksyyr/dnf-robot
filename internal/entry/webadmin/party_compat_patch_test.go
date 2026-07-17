package webadmin

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

func testPartyCompatLayout() partyCompatLayout {
	return partyCompatLayout{site: 64, cave: 128, rawSend: 320, resumeSite: 74}
}

func TestPartyCompatDefaultRangeCoversFirstThousandRobotAccounts(t *testing.T) {
	if defaultPartyCompatAccountStart != 17000000 || defaultPartyCompatAccountEnd != 17001000 {
		t.Fatalf("default range = %d..%d", defaultPartyCompatAccountStart, defaultPartyCompatAccountEnd)
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
	wantCave, _ := hex.DecodeString("8b45088b80ac0407003d406603010f820b0000003d80a812010f82e00db5ff807dd3000f84d60db5ffe9120bb5ff")
	if !bytes.Equal(cave, wantCave) {
		t.Fatalf("cave = %x, want %x", cave, wantCave)
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
}
