package webadmin

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var errPartyCompatUnavailable = errors.New("df_game_r is not listening")

func inspectPartyCompat(port int, cfg partyCompatConfig) partyCompatStatus {
	status := partyCompatStatus{State: "unavailable", Port: port, AccountStart: cfg.AccountStart, AccountEnd: cfg.AccountEnd}
	pid, err := gamePIDForPort(port)
	if err != nil {
		status.Message = err.Error()
		status.processUnavailable = errors.Is(err, errPartyCompatUnavailable)
		return status
	}
	status.PID = pid
	mem, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status
	}
	defer mem.Close()
	enabled, start, end, err := inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
	if err != nil {
		status.State = "unknown"
		status.Message = err.Error()
		return status
	}
	status.Enabled = enabled
	status.orphanedCave = !enabled && start != 0 && end != 0
	if enabled {
		status.State = "on"
		status.AccountStart = start
		status.AccountEnd = end
	} else {
		status.State = "off"
	}
	return status
}

func setPartyCompat(port int, cfg partyCompatConfig, enable bool) (partyCompatStatus, error) {
	status := partyCompatStatus{Port: port, AccountStart: cfg.AccountStart, AccountEnd: cfg.AccountEnd}
	pid, err := gamePIDForPort(port)
	if err != nil {
		return status, err
	}
	status.PID = pid
	mem, err := os.OpenFile(fmt.Sprintf("/proc/%d/mem", pid), os.O_RDWR, 0)
	if err != nil {
		return status, err
	}
	defer mem.Close()

	if err := withStoppedProcess(pid, func() error {
		_, err := setPartyCompatMemory(mem, defaultPartyCompatLayout, cfg.AccountStart, cfg.AccountEnd, enable)
		return err
	}); err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status, err
	}

	enabled, start, end, err := inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
	if err != nil {
		return status, err
	}
	status.Enabled = enabled
	status.State = "off"
	if enabled {
		status.State = "on"
		status.AccountStart = start
		status.AccountEnd = end
	}
	if enabled != enable {
		return status, fmt.Errorf("party compatibility patch verification failed")
	}
	return status, nil
}

func gamePIDForPort(port int) (int, error) {
	data, err := exec.Command("ss", "-lntp").Output()
	if err != nil {
		return 0, fmt.Errorf("read listening ports: %w", err)
	}
	pid, err := parseGamePIDForPort(data, port)
	if err != nil {
		return 0, err
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%w: pid %d exited", errPartyCompatUnavailable, pid)
		}
		return 0, err
	}
	for _, part := range strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00") {
		if filepath.Base(part) == "df_game_r" {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("pid %d on port %d is not df_game_r", pid, port)
}

func parseGamePIDForPort(data []byte, port int) (int, error) {
	portPattern := regexp.MustCompile(`:` + regexp.QuoteMeta(strconv.Itoa(port)) + `\s`)
	pidPattern := regexp.MustCompile(`pid=([0-9]+)`)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !portPattern.Match(line) {
			continue
		}
		match := pidPattern.FindSubmatch(line)
		if len(match) != 2 {
			continue
		}
		pid, err := strconv.Atoi(string(match[1]))
		if err == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("%w on port %d", errPartyCompatUnavailable, port)
}
