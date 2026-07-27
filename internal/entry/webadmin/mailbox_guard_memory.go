package webadmin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

var defaultMailboxGuardLayout = mailboxGuardLayout{
	invalidItemScanSite: 0x0867dcb6,
	streamListEmptySite: 0x0855839e,
}

type mailboxGuardLayout struct {
	invalidItemScanSite int64
	streamListEmptySite int64
}

var (
	mailboxInvalidItemScanOriginal = []byte{0x85, 0xc0}
	mailboxInvalidItemScanPatched  = []byte{0x31, 0xc0}
	mailboxInvalidItemScanPrefix   = []byte{0x8b, 0x45, 0x08, 0x8b, 0x80, 0x98, 0x1b, 0x07, 0x00}
	mailboxInvalidItemScanSuffix   = []byte{0x0f, 0x84, 0xb3, 0x03, 0x00, 0x00}

	mailboxStreamListEmptyOriginal = []byte{0x55, 0x89, 0xe5, 0x8b, 0x45, 0x08, 0x8b, 0x10, 0x8b, 0x45, 0x08, 0x39, 0xc2, 0x0f, 0x94, 0xc0, 0x5d, 0xc3}
	// Treat a zero list head as empty before applying the normal sentinel comparison.
	// This keeps valid std::list<Stream*> behavior unchanged and prevents malformed
	// mailbox state from dereferencing address 0x4 in ReqDBSendStoredMail.
	mailboxStreamListEmptyPatched = []byte{0x8b, 0x44, 0x24, 0x04, 0x8b, 0x10, 0x85, 0xd2, 0x0f, 0x44, 0xd0, 0x39, 0xc2, 0x0f, 0x94, 0xc0, 0xc3, 0x90}
	mailboxStreamListEmptyPrefix  = []byte{0xc9, 0xc3, 0x90}
	mailboxStreamListEmptySuffix  = []byte{0x55, 0x89, 0xe5, 0x53, 0x83, 0xec, 0x14}
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
	enabled, err := inspectMailboxGuardMemory(mem, defaultMailboxGuardLayout)
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
		_, err := setMailboxGuardMemory(mem, defaultMailboxGuardLayout, enable)
		return err
	}); err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status, err
	}
	actual, err := inspectMailboxGuardMemory(mem, defaultMailboxGuardLayout)
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

func inspectMailboxGuardMemory(mem io.ReaderAt, layout mailboxGuardLayout) (bool, error) {
	invalidItemScan, err := validateMailboxInvalidItemScanTarget(mem, layout.invalidItemScanSite)
	if err != nil {
		return false, err
	}
	streamListEmpty, err := validateMailboxStreamListEmptyTarget(mem, layout.streamListEmptySite)
	if err != nil {
		return false, err
	}
	invalidItemScanEnabled := bytes.Equal(invalidItemScan, mailboxInvalidItemScanPatched)
	streamListEmptyEnabled := bytes.Equal(streamListEmpty, mailboxStreamListEmptyPatched)
	if invalidItemScanEnabled != streamListEmptyEnabled {
		return false, fmt.Errorf("mailbox bad-node guard is partially applied: invalid_item_scan=%t stream_list_empty=%t", invalidItemScanEnabled, streamListEmptyEnabled)
	}
	return invalidItemScanEnabled, nil
}

func setMailboxGuardMemory(mem memoryReadWriter, layout mailboxGuardLayout, enable bool) (bool, error) {
	invalidItemScanBefore, err := validateMailboxInvalidItemScanTarget(mem, layout.invalidItemScanSite)
	if err != nil {
		return false, err
	}
	streamListEmptyBefore, err := validateMailboxStreamListEmptyTarget(mem, layout.streamListEmptySite)
	if err != nil {
		return false, err
	}
	invalidItemScanDesired := mailboxInvalidItemScanOriginal
	streamListEmptyDesired := mailboxStreamListEmptyOriginal
	if enable {
		invalidItemScanDesired = mailboxInvalidItemScanPatched
		streamListEmptyDesired = mailboxStreamListEmptyPatched
	}
	changeInvalidItemScan := !bytes.Equal(invalidItemScanBefore, invalidItemScanDesired)
	changeStreamListEmpty := !bytes.Equal(streamListEmptyBefore, streamListEmptyDesired)
	if !changeInvalidItemScan && !changeStreamListEmpty {
		return false, nil
	}
	if changeStreamListEmpty {
		if err := writeMemoryVerified(mem, layout.streamListEmptySite, streamListEmptyDesired); err != nil {
			return false, err
		}
	}
	if changeInvalidItemScan {
		if err := writeMemoryVerified(mem, layout.invalidItemScanSite, invalidItemScanDesired); err != nil {
			if changeStreamListEmpty {
				_ = writeMemoryVerified(mem, layout.streamListEmptySite, streamListEmptyBefore)
			}
			return false, err
		}
	}
	actual, err := inspectMailboxGuardMemory(mem, layout)
	if err != nil || actual != enable {
		if changeInvalidItemScan {
			_ = writeMemoryVerified(mem, layout.invalidItemScanSite, invalidItemScanBefore)
		}
		if changeStreamListEmpty {
			_ = writeMemoryVerified(mem, layout.streamListEmptySite, streamListEmptyBefore)
		}
		if err != nil {
			return false, err
		}
		return false, fmt.Errorf("mailbox bad-node guard memory verification failed")
	}
	return true, nil
}

func validateMailboxInvalidItemScanTarget(mem io.ReaderAt, site int64) ([]byte, error) {
	prefix, err := readMemory(mem, site-int64(len(mailboxInvalidItemScanPrefix)), len(mailboxInvalidItemScanPrefix))
	if err != nil {
		return nil, err
	}
	current, err := readMemory(mem, site, len(mailboxInvalidItemScanOriginal))
	if err != nil {
		return nil, err
	}
	suffix, err := readMemory(mem, site+int64(len(mailboxInvalidItemScanOriginal)), len(mailboxInvalidItemScanSuffix))
	if err != nil {
		return nil, err
	}
	knownPatch := bytes.Equal(current, mailboxInvalidItemScanOriginal) || bytes.Equal(current, mailboxInvalidItemScanPatched)
	if !bytes.Equal(prefix, mailboxInvalidItemScanPrefix) || !knownPatch || !bytes.Equal(suffix, mailboxInvalidItemScanSuffix) {
		return nil, fmt.Errorf("unsupported df_game_r near mailbox invalid-item scan site: %x%x%x", prefix, current, suffix)
	}
	return current, nil
}

func validateMailboxStreamListEmptyTarget(mem io.ReaderAt, site int64) ([]byte, error) {
	prefix, err := readMemory(mem, site-int64(len(mailboxStreamListEmptyPrefix)), len(mailboxStreamListEmptyPrefix))
	if err != nil {
		return nil, err
	}
	current, err := readMemory(mem, site, len(mailboxStreamListEmptyOriginal))
	if err != nil {
		return nil, err
	}
	suffix, err := readMemory(mem, site+int64(len(mailboxStreamListEmptyOriginal)), len(mailboxStreamListEmptySuffix))
	if err != nil {
		return nil, err
	}
	knownPatch := bytes.Equal(current, mailboxStreamListEmptyOriginal) || bytes.Equal(current, mailboxStreamListEmptyPatched)
	if !bytes.Equal(prefix, mailboxStreamListEmptyPrefix) || !knownPatch || !bytes.Equal(suffix, mailboxStreamListEmptySuffix) {
		return nil, fmt.Errorf("unsupported df_game_r near mailbox stream-list empty site: %x%x%x", prefix, current, suffix)
	}
	return current, nil
}
