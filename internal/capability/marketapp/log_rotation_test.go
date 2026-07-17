package marketapp

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAppendLogUsesConfiguredRotation(t *testing.T) {
	app := &App{
		configDir:   t.TempDir(),
		logMaxBytes: 256,
		logBackups:  1,
	}
	for index := 0; index < 8; index++ {
		app.appendLog(LogEvent{Type: "rotation", Message: strings.Repeat("x", 80)})
	}
	path := marketLogPath(app.configDir)
	assertLogFilesBounded(t, path, 256, 1)
}

func TestMarketServiceFailureIgnoresHistoricalBackup(t *testing.T) {
	app := &App{logMaxBytes: 1024, logBackups: 1}
	dir := t.TempDir()
	path := filepath.Join(dir, "service.log")
	if err := os.WriteFile(path, []byte("healthy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backup := path + ".1"
	if err := os.WriteFile(backup, []byte("fatal old failure\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if app.hasMarketServiceFailure(path) {
		t.Fatal("historical backup contaminated current startup health")
	}
	if err := os.WriteFile(path, []byte("fatal current failure\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !app.hasMarketServiceFailure(path) {
		t.Fatal("current startup failure was not detected")
	}
}

func TestMarketServiceCommandTruncatesThenOpensAppend(t *testing.T) {
	command := marketServiceShellCommand("./service", []string{"start"}, "/tmp/service.log", "", 256, 5)
	if !strings.HasPrefix(command, ": >'/tmp/service.log'; ") {
		t.Fatalf("command does not clear the prior run: %s", command)
	}
	if !strings.Contains(command, " >>'/tmp/service.log' 2>&1 &") {
		t.Fatalf("command does not use append mode: %s", command)
	}
}

func TestMarketServiceCommandUsesBoundedSink(t *testing.T) {
	command := marketServiceShellCommand("./service", []string{"start"}, "/tmp/service.log", "/root/robot", 100*1024*1024, 5)
	if !strings.Contains(command, "sh -c") || !strings.Contains(command, "--bounded-log-sink") {
		t.Fatalf("command does not use bounded sink: %s", command)
	}
	if !strings.Contains(command, "--bounded-log-max-bytes 104857600") || !strings.Contains(command, "--bounded-log-backups 5") {
		t.Fatalf("command does not pass configured limits: %s", command)
	}
}

func assertLogFilesBounded(t *testing.T, path string, maxBytes int64, backups int) {
	t.Helper()
	for index := 0; index <= backups; index++ {
		candidate := path
		if index > 0 {
			candidate = filepath.Clean(path + "." + strconv.Itoa(index))
		}
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatalf("stat %s: %v", candidate, err)
		}
		if info.Size() > maxBytes {
			t.Fatalf("%s exceeds limit: %d", candidate, info.Size())
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}
