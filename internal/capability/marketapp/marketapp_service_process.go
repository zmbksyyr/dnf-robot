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

	"robot/internal/foundation/logfile"
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
		if err := a.stopMarketServiceLogSink(name); err != nil {
			return err
		}
		a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopSkipped, Message: "process and port are already down"})
		return nil
	}
	_ = exec.Command("pkill", "-TERM", "-x", process).Run()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			if err := a.stopMarketServiceLogSink(name); err != nil {
				return err
			}
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusStopped, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-x", process).Run()
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if marketServicePID(bin) <= 0 && !tcpReady(addr, 200*time.Millisecond) {
			if err := a.stopMarketServiceLogSink(name); err != nil {
				return err
			}
			a.appendLog(LogEvent{Type: "market_service", Market: name, Status: marketLogStatusKilled, Message: process})
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s stop timeout: %s still running or port still listening", name, process)
}

func (a *App) stopMarketServiceLogSink(name string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	sinkBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve bounded log sink: %w", err)
	}
	pattern := "^" + regexp.QuoteMeta(sinkBin) + " --bounded-log-sink " + regexp.QuoteMeta(a.marketServiceLogPath(name)) + "( |$)"
	_ = exec.Command("pkill", "-TERM", "-f", pattern).Run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("pgrep", "-f", pattern).Output()
		if len(strings.Fields(string(out))) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-f", pattern).Run()
	out, _ := exec.Command("pgrep", "-f", pattern).Output()
	if len(strings.Fields(string(out))) > 0 {
		return fmt.Errorf("%s bounded log sink did not stop", name)
	}
	return nil
}

func (a *App) marketServiceLogPath(name string) string {
	if a.configDir == "" {
		return filepath.Join(os.TempDir(), "robot_market_"+name+".log")
	}
	return filepath.Join(a.configDir, "market_"+name+"_service.log")
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

func marketServiceLogSinkRunning(sinkBin, outputPath string, maxBytes int64, backups int) bool {
	if runtime.GOOS != "linux" || strings.TrimSpace(sinkBin) == "" {
		return true
	}
	pattern := "^" + regexp.QuoteMeta(sinkBin) + " --bounded-log-sink " + regexp.QuoteMeta(outputPath) +
		" --bounded-log-max-bytes " + strconv.FormatInt(maxBytes, 10) +
		" --bounded-log-backups " + strconv.Itoa(backups) + "$"
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	return err == nil && len(strings.Fields(string(out))) > 0
}

func (a *App) hasMarketServiceFailure(logPath string) bool {
	maxBytes, _ := a.logLimits()
	found, err := logfile.ContainsAnyTail(logPath, maxBytes,
		"fail to registitem",
		"process exits",
		"fatal",
	)
	return err == nil && found
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
