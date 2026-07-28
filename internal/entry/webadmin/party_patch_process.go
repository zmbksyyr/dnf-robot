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

	processfoundation "robot/internal/foundation/process"
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
	var enabled, rewardTimerEnabled bool
	var start, end uint32
	err = processfoundation.WithMemoryFile(pid, false, defaultPartyCompatLayout.site, func(mem processfoundation.MemoryFile, _ bool) error {
		var inspectErr error
		enabled, start, end, inspectErr = inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
		if inspectErr != nil {
			return inspectErr
		}
		rewardTimerEnabled, inspectErr = inspectPartyCompatRewardTimer(mem, defaultPartyCompatLayout)
		return inspectErr
	})
	if err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status
	}
	status.Enabled = enabled && rewardTimerEnabled
	status.orphanedCave = !status.Enabled && (enabled || rewardTimerEnabled || start != 0 && end != 0)
	if status.Enabled {
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
	var enabled, rewardTimerEnabled bool
	var start, end uint32
	err = processfoundation.WithMemoryFile(pid, true, defaultPartyCompatLayout.site, func(mem processfoundation.MemoryFile, traced bool) error {
		apply := func() error {
			_, err := setPartyCompatMemory(mem, defaultPartyCompatLayout, cfg.AccountStart, cfg.AccountEnd, enable)
			return err
		}
		if traced {
			if err := apply(); err != nil {
				return err
			}
		} else if err := withStoppedProcess(pid, apply); err != nil {
			return err
		}
		var inspectErr error
		enabled, start, end, inspectErr = inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
		if inspectErr != nil {
			return inspectErr
		}
		rewardTimerEnabled, inspectErr = inspectPartyCompatRewardTimer(mem, defaultPartyCompatLayout)
		return inspectErr
	})
	if err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status, err
	}
	status.Enabled = enabled && rewardTimerEnabled
	status.State = "off"
	if status.Enabled {
		status.State = "on"
		status.AccountStart = start
		status.AccountEnd = end
	}
	if status.Enabled != enable {
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
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !portPattern.Match(line) {
			continue
		}
		_, pid, ok := parseSSProcess(string(line))
		if ok {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("%w on port %d", errPartyCompatUnavailable, port)
}

var (
	modernSSProcessPattern = regexp.MustCompile(`"([^"]+)",pid=([0-9]+)`)
	legacySSProcessPattern = regexp.MustCompile(`"([^"]+)",([0-9]+),`)
)

func parseSSProcess(line string) (string, int, bool) {
	for _, pattern := range []*regexp.Regexp{modernSSProcessPattern, legacySSProcessPattern} {
		match := pattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		pid, err := strconv.Atoi(match[2])
		if err == nil && pid > 0 {
			return match[1], pid, true
		}
	}
	return "", 0, false
}
