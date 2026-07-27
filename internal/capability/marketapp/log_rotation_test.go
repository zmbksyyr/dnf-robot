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

func TestMarketServiceCommandLeavesLogsToService(t *testing.T) {
	command := marketServiceShellCommand("./service", []string{"start"})
	if command != "nohup './service' 'start' >/dev/null 2>&1 &" {
		t.Fatalf("service command = %s", command)
	}
	if strings.Contains(command, "bounded-log-sink") || strings.Contains(command, "service.log") {
		t.Fatalf("service command retained robot log ownership: %s", command)
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
