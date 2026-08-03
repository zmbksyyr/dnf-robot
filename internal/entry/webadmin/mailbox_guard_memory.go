package webadmin

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	processfoundation "robot/internal/foundation/process"
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
	errMailboxGuardUnsupported = errors.New("unsupported df_game_r mailbox guard layout")
	errMailboxGuardPartial     = errors.New("mailbox bad-node guard is partially applied")

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
	var enabled bool
	err = processfoundation.WithMemoryFile(pid, false, defaultMailboxGuardLayout.invalidItemScanSite, func(mem processfoundation.MemoryFile, _ bool) error {
		var inspectErr error
		enabled, inspectErr = inspectMailboxGuardMemory(mem, defaultMailboxGuardLayout)
		return inspectErr
	})
	if err != nil {
		switch {
		case errors.Is(err, errMailboxGuardUnsupported):
			status.State = "unsupported"
		case errors.Is(err, errMailboxGuardPartial):
			status.State = "partial"
		default:
			status.State = "error"
		}
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
	var actual bool
	err = processfoundation.WithMemoryFile(pid, true, defaultMailboxGuardLayout.invalidItemScanSite, func(mem processfoundation.MemoryFile, traced bool) error {
		apply := func() error {
			_, err := setMailboxGuardMemory(mem, defaultMailboxGuardLayout, enable)
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
		actual, inspectErr = inspectMailboxGuardMemory(mem, defaultMailboxGuardLayout)
		return inspectErr
	})
	if err != nil {
		status.State = "error"
		status.Message = err.Error()
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
		return false, fmt.Errorf("%w: invalid_item_scan=%t stream_list_empty=%t", errMailboxGuardPartial, invalidItemScanEnabled, streamListEmptyEnabled)
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
			rollbackErr := restoreMemoryVerified(mem, memoryRestore{address: layout.streamListEmptySite, value: streamListEmptyBefore})
			return false, memoryPatchError("write mailbox stream-list guard", err, rollbackErr)
		}
	}
	if changeInvalidItemScan {
		if err := writeMemoryVerified(mem, layout.invalidItemScanSite, invalidItemScanDesired); err != nil {
			restores := []memoryRestore{{address: layout.invalidItemScanSite, value: invalidItemScanBefore}}
			if changeStreamListEmpty {
				restores = append(restores, memoryRestore{address: layout.streamListEmptySite, value: streamListEmptyBefore})
			}
			rollbackErr := restoreMemoryVerified(mem, restores...)
			return false, memoryPatchError("write mailbox invalid-item guard", err, rollbackErr)
		}
	}
	actual, err := inspectMailboxGuardMemory(mem, layout)
	if err != nil || actual != enable {
		verificationErr := err
		if verificationErr == nil {
			verificationErr = fmt.Errorf("mailbox bad-node guard memory verification failed")
		}
		restores := make([]memoryRestore, 0, 2)
		if changeInvalidItemScan {
			restores = append(restores, memoryRestore{address: layout.invalidItemScanSite, value: invalidItemScanBefore})
		}
		if changeStreamListEmpty {
			restores = append(restores, memoryRestore{address: layout.streamListEmptySite, value: streamListEmptyBefore})
		}
		return false, memoryPatchError("verify mailbox bad-node guard", verificationErr, restoreMemoryVerified(mem, restores...))
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
		return nil, fmt.Errorf("%w near invalid-item scan site: %x%x%x", errMailboxGuardUnsupported, prefix, current, suffix)
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
		return nil, fmt.Errorf("%w near stream-list empty site: %x%x%x", errMailboxGuardUnsupported, prefix, current, suffix)
	}
	return current, nil
}
