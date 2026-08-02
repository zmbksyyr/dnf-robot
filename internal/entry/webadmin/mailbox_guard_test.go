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
	"robot/internal/foundation/layout"
)

func newMailboxGuardMemory(t *testing.T) (*os.File, mailboxGuardLayout) {
	t.Helper()
	layout := mailboxGuardLayout{invalidItemScanSite: 64, streamListEmptySite: 160}
	file, err := os.CreateTemp(t.TempDir(), "mailbox-guard-mem")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(layout.streamListEmptySite + 64); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxInvalidItemScanPrefix, layout.invalidItemScanSite-int64(len(mailboxInvalidItemScanPrefix))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxInvalidItemScanOriginal, layout.invalidItemScanSite); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxInvalidItemScanSuffix, layout.invalidItemScanSite+int64(len(mailboxInvalidItemScanOriginal))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxStreamListEmptyPrefix, layout.streamListEmptySite-int64(len(mailboxStreamListEmptyPrefix))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxStreamListEmptyOriginal, layout.streamListEmptySite); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(mailboxStreamListEmptySuffix, layout.streamListEmptySite+int64(len(mailboxStreamListEmptyOriginal))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file, layout
}

func TestMailboxGuardConfigRequiresReleasedFile(t *testing.T) {
	dir := t.TempDir()
	s := New(&config.SysConfig{ConfigDir: dir}, "", "")
	if _, err := s.loadMailboxGuardConfig(); err == nil {
		t.Fatal("missing mailbox guard config unexpectedly accepted")
	}
	if err := layout.New(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.mailboxGuardConfigPath(), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.loadMailboxGuardConfig(); err == nil {
		t.Fatal("mailbox guard config without required switch unexpectedly accepted")
	}
	if err := os.WriteFile(s.mailboxGuardConfigPath(), []byte(`{"mailbox_bad_node_guard":false,"legacy":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.loadMailboxGuardConfig(); err == nil {
		t.Fatal("mailbox guard config with unknown field unexpectedly accepted")
	}
}

func TestMailboxGuardHandlerRejectsMissingUnknownAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	if err := layout.New(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.New(dir).MailboxGuard(), []byte(`{"mailbox_bad_node_guard":false}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := New(&config.SysConfig{ConfigDir: dir, RobotGamePort: 65534}, "", "")
	for _, body := range []string{
		`{}`,
		`{"mailbox_bad_node_guard":true,"legacy":1}`,
		`{"mailbox_bad_node_guard":true}{"mailbox_bad_node_guard":false}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/compat", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		s.handleCompat(recorder, req)
		var response struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.OK {
			t.Fatalf("invalid request unexpectedly accepted: body=%s response=%s", body, recorder.Body.String())
		}
	}
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil || cfg.Enabled {
		t.Fatalf("invalid request changed config=%+v err=%v", cfg, err)
	}
}

func TestMailboxGuardHandlerAppliesOrQueuesDesiredState(t *testing.T) {
	dir := t.TempDir()
	if err := layout.New(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.New(dir).MailboxGuard(), []byte(`{"mailbox_bad_node_guard":false}`), 0644); err != nil {
		t.Fatal(err)
	}
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
	if !response.OK || response.RestartRequired {
		t.Fatalf("response = %s", recorder.Body.String())
	}
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil || !cfg.Enabled {
		t.Fatalf("saved config = %+v err=%v", cfg, err)
	}
}

func TestSetMailboxGuardMemoryOnOff(t *testing.T) {
	mem, layout := newMailboxGuardMemory(t)
	changed, err := setMailboxGuardMemory(mem, layout, true)
	if err != nil || !changed {
		t.Fatalf("enable changed=%t err=%v", changed, err)
	}
	enabled, err := inspectMailboxGuardMemory(mem, layout)
	if err != nil || !enabled {
		t.Fatalf("enabled=%t err=%v", enabled, err)
	}
	changed, err = setMailboxGuardMemory(mem, layout, false)
	if err != nil || !changed {
		t.Fatalf("disable changed=%t err=%v", changed, err)
	}
	enabled, err = inspectMailboxGuardMemory(mem, layout)
	if err != nil || enabled {
		t.Fatalf("enabled=%t err=%v", enabled, err)
	}
}

func TestMailboxGuardRejectsUnknownServerVersion(t *testing.T) {
	mem, layout := newMailboxGuardMemory(t)
	if _, err := mem.WriteAt([]byte{0x90}, layout.streamListEmptySite-int64(len(mailboxStreamListEmptyPrefix))); err != nil {
		t.Fatal(err)
	}
	if _, err := setMailboxGuardMemory(mem, layout, true); err == nil || !strings.Contains(err.Error(), "unsupported df_game_r") {
		t.Fatalf("patch error = %v", err)
	}
	got, err := readMemory(mem, layout.invalidItemScanSite, len(mailboxInvalidItemScanOriginal))
	if err != nil || !bytes.Equal(got, mailboxInvalidItemScanOriginal) {
		t.Fatalf("unknown target was modified: %x err=%v", got, err)
	}
}

func TestMailboxGuardRejectsPartialPatch(t *testing.T) {
	mem, layout := newMailboxGuardMemory(t)
	if _, err := mem.WriteAt(mailboxInvalidItemScanPatched, layout.invalidItemScanSite); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectMailboxGuardMemory(mem, layout); err == nil || !strings.Contains(err.Error(), "partially applied") {
		t.Fatalf("partial patch error = %v", err)
	}
}

func TestMailboxStreamListEmptyPatchFitsOriginalFunction(t *testing.T) {
	if len(mailboxStreamListEmptyPatched) != len(mailboxStreamListEmptyOriginal) {
		t.Fatalf("patched function is %d bytes, want %d", len(mailboxStreamListEmptyPatched), len(mailboxStreamListEmptyOriginal))
	}
	want := []byte{0x8b, 0x44, 0x24, 0x04, 0x8b, 0x10, 0x85, 0xd2, 0x0f, 0x44, 0xd0, 0x39, 0xc2, 0x0f, 0x94, 0xc0, 0xc3, 0x90}
	if !bytes.Equal(mailboxStreamListEmptyPatched, want) {
		t.Fatalf("patched function = %x, want %x", mailboxStreamListEmptyPatched, want)
	}
}

func TestCompatButtonPrecedesPorts(t *testing.T) {
	compatAt := strings.Index(indexHTML, `id="compatButton"`)
	portsAt := strings.Index(indexHTML, `onclick="openPortsDialog()"`)
	if compatAt < 0 || portsAt < 0 || compatAt >= portsAt {
		t.Fatalf("Compat button must be before Ports: compat=%d ports=%d", compatAt, portsAt)
	}
}
