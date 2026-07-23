package webadmin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const mailboxGuardPatchSite int64 = 0x0867dcb6

var (
	mailboxGuardOriginal = []byte{0x85, 0xc0}
	mailboxGuardPatched  = []byte{0x31, 0xc0}
	mailboxGuardPrefix   = []byte{0x8b, 0x45, 0x08, 0x8b, 0x80, 0x98, 0x1b, 0x07, 0x00}
	mailboxGuardSuffix   = []byte{0x0f, 0x84, 0xb3, 0x03, 0x00, 0x00}
)

func inspectMailboxGuard(port int) mailboxGuardStatus {
	status := mailboxGuardStatus{State: "unavailable", Port: port}
	pid, err := gamePIDForPort(port)
	if err != nil {
		if !errors.Is(err, errPartyCompatUnavailable) {
			status.State = "error"
		}
		status.Message = err.Error()
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
	enabled, err := inspectMailboxGuardMemory(mem, mailboxGuardPatchSite)
	if err != nil {
		status.State = "unsupported"
		status.Message = err.Error()
		return status
	}
	status.Enabled = enabled
	status.State = "off"
	if enabled {
		status.State = "on"
	}
	return status
}

func setMailboxGuard(port int, enable bool) (mailboxGuardStatus, error) {
	status := mailboxGuardStatus{Port: port}
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
		_, err := setMailboxGuardMemory(mem, mailboxGuardPatchSite, enable)
		return err
	}); err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status, err
	}
	actual, err := inspectMailboxGuardMemory(mem, mailboxGuardPatchSite)
	if err != nil {
		return status, err
	}
	status.Enabled = actual
	status.State = "off"
	if actual {
		status.State = "on"
	}
	if actual != enable {
		return status, fmt.Errorf("mailbox bad-node guard verification failed")
	}
	return status, nil
}

func inspectMailboxGuardMemory(mem io.ReaderAt, site int64) (bool, error) {
	current, err := validateMailboxGuardTarget(mem, site)
	if err != nil {
		return false, err
	}
	return bytes.Equal(current, mailboxGuardPatched), nil
}

func setMailboxGuardMemory(mem memoryReadWriter, site int64, enable bool) (bool, error) {
	current, err := validateMailboxGuardTarget(mem, site)
	if err != nil {
		return false, err
	}
	desired := mailboxGuardOriginal
	if enable {
		desired = mailboxGuardPatched
	}
	if bytes.Equal(current, desired) {
		return false, nil
	}
	if err := writeMemoryVerified(mem, site, desired); err != nil {
		return false, err
	}
	return true, nil
}

func validateMailboxGuardTarget(mem io.ReaderAt, site int64) ([]byte, error) {
	prefix, err := readMemory(mem, site-int64(len(mailboxGuardPrefix)), len(mailboxGuardPrefix))
	if err != nil {
		return nil, err
	}
	current, err := readMemory(mem, site, len(mailboxGuardOriginal))
	if err != nil {
		return nil, err
	}
	suffix, err := readMemory(mem, site+int64(len(mailboxGuardOriginal)), len(mailboxGuardSuffix))
	if err != nil {
		return nil, err
	}
	knownPatch := bytes.Equal(current, mailboxGuardOriginal) || bytes.Equal(current, mailboxGuardPatched)
	if !bytes.Equal(prefix, mailboxGuardPrefix) || !knownPatch || !bytes.Equal(suffix, mailboxGuardSuffix) {
		return nil, fmt.Errorf("unsupported df_game_r near mailbox guard site: %x%x%x", prefix, current, suffix)
	}
	return current, nil
}
