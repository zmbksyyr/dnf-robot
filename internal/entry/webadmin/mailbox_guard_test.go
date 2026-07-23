package webadmin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"robot/internal/foundation/config"
)

func newMailboxGuardMemory(t *testing.T, site int64) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "mailbox-guard-mem")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(site + 64); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxGuardPrefix, site-int64(len(mailboxGuardPrefix))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxGuardOriginal, site); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxGuardSuffix, site+int64(len(mailboxGuardOriginal))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestMailboxGuardConfigDefaultsOff(t *testing.T) {
	s := New(&config.SysConfig{ConfigDir: t.TempDir()}, "", "")
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("mailbox guard defaulted on")
	}
}

func TestMailboxGuardHandlerOnlySavesDesiredState(t *testing.T) {
	dir := t.TempDir()
	s := New(&config.SysConfig{ConfigDir: dir, RobotGamePort: 65534}, "", "")
	req := httptest.NewRequest(http.MethodPost, "/api/compat", strings.NewReader(`{"mailbox_bad_node_guard":true}`))
	recorder := httptest.NewRecorder()
	s.handleCompat(recorder, req)

	var response struct {
		OK              bool `json:"ok"`
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.RestartRequired {
		t.Fatalf("response = %s", recorder.Body.String())
	}
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil || !cfg.Enabled {
		t.Fatalf("saved config = %+v err=%v", cfg, err)
	}
}

func TestSetMailboxGuardMemoryOnOff(t *testing.T) {
	const site int64 = 64
	mem := newMailboxGuardMemory(t, site)
	changed, err := setMailboxGuardMemory(mem, site, true)
	if err != nil || !changed {
		t.Fatalf("enable changed=%t err=%v", changed, err)
	}
	enabled, err := inspectMailboxGuardMemory(mem, site)
	if err != nil || !enabled {
		t.Fatalf("enabled=%t err=%v", enabled, err)
	}
	changed, err = setMailboxGuardMemory(mem, site, false)
	if err != nil || !changed {
		t.Fatalf("disable changed=%t err=%v", changed, err)
	}
	enabled, err = inspectMailboxGuardMemory(mem, site)
	if err != nil || enabled {
		t.Fatalf("enabled=%t err=%v", enabled, err)
	}
}

func TestMailboxGuardRejectsUnknownServerVersion(t *testing.T) {
	const site int64 = 64
	mem := newMailboxGuardMemory(t, site)
	if _, err := mem.WriteAt([]byte{0x90}, site-int64(len(mailboxGuardPrefix))); err != nil {
		t.Fatal(err)
	}
	if _, err := setMailboxGuardMemory(mem, site, true); err == nil || !strings.Contains(err.Error(), "unsupported df_game_r") {
		t.Fatalf("patch error = %v", err)
	}
	got, err := readMemory(mem, site, len(mailboxGuardOriginal))
	if err != nil || !bytes.Equal(got, mailboxGuardOriginal) {
		t.Fatalf("unknown target was modified: %x err=%v", got, err)
	}
}

func TestCompatButtonPrecedesPorts(t *testing.T) {
	compatAt := strings.Index(indexHTML, `id="compatButton"`)
	portsAt := strings.Index(indexHTML, `onclick="openPortsDialog()"`)
	if compatAt < 0 || portsAt < 0 || compatAt >= portsAt {
		t.Fatalf("Compat button must be before Ports: compat=%d ports=%d", compatAt, portsAt)
	}
}
