package marketapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (a *App) dfGameRRunning() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	name := filepath.Base(strings.TrimSpace(a.dfGameR))
	if name == "." || name == "/" || name == "" {
		name = "df_game_r"
	}
	out, err := exec.Command("pidof", name).Output()
	if err == nil && len(strings.Fields(string(out))) > 0 {
		return true
	}
	out, err = exec.Command("pgrep", "-f", "(^|/)"+regexp.QuoteMeta(name)+"( |$)").Output()
	return err == nil && len(strings.Fields(string(out))) > 0
}

func (a *App) stopMarketServiceForItemInfo(name, addr, bin string) error {
	process := filepath.Base(strings.TrimSpace(bin))
	if process == "" || process == "." || process == "/" {
		return fmt.Errorf("%s stop failed: invalid process name %q", name, bin)
	}
	pid := marketServicePID(bin)
	if pid <= 0 && !tcpReady(addr, 200*time.Millisecond) {
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopSkipped, Message: "process and port are already down"})
		return nil
	}
	// The server's /root/stop script uses SIGKILL for these legacy services.
	// They do not reliably handle SIGTERM, so waiting for graceful shutdown adds
	// a fixed delay before reaching the same result.
	_ = exec.Command("pkill", "-KILL", "-x", process).Run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusKilled, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s stop timeout: %s still running or port still listening", name, process)
}

func marketServicePID(bin string) int {
	name := filepath.Base(strings.TrimSpace(bin))
	if name == "" || name == "." || name == "/" {
		return 0
	}
	out, err := exec.Command("pidof", name).Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return pid
}

func prepareMarketServiceDir(dir string) error {
	if err := os.Chmod(dir, 0777); err != nil && !os.IsPermission(err) {
		return err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "pid", "*.pid"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
