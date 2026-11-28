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
	initBranchSite:      0x08551da9,
}

type mailboxGuardLayout struct {
	invalidItemScanSite int64
	streamListEmptySite int64
	initBranchSite      int64
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

	// CMailBox::Init skips every scalar reset when the pooled loaded byte is
	// not 1. Redirect that branch to the existing reset block, preserving the
	// loaded-mail cleanup path. Otherwise a stale postal cursor hides new mail.
	mailboxInitBranchOriginal = []byte{0x0f, 0x85, 0x98, 0x01, 0x00, 0x00}
	mailboxInitBranchPatched  = []byte{0x0f, 0x85, 0xa3, 0x00, 0x00, 0x00}
	mailboxInitBranchPrefix   = []byte{0x8b, 0x45, 0x08, 0x0f, 0xb6, 0x00, 0x83, 0xf0, 0x01, 0x84, 0xc0}
	mailboxInitResetPrefix    = []byte{0x8b, 0x45, 0x08, 0xc6, 0x00, 0x00, 0x8b, 0x45, 0x08, 0xc7, 0x40, 0x04, 0x00, 0x00, 0x00, 0x00}
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
	initBranch, err := validateMailboxInitBranchTarget(mem, layout.initBranchSite)
	if err != nil {
		return false, err
	}
	invalidItemScanEnabled := bytes.Equal(invalidItemScan, mailboxInvalidItemScanPatched)
	streamListEmptyEnabled := bytes.Equal(streamListEmpty, mailboxStreamListEmptyPatched)
	initBranchEnabled := bytes.Equal(initBranch, mailboxInitBranchPatched)
	if invalidItemScanEnabled != streamListEmptyEnabled || invalidItemScanEnabled != initBranchEnabled {
		return false, fmt.Errorf("%w: invalid_item_scan=%t stream_list_empty=%t init_reset=%t", errMailboxGuardPartial, invalidItemScanEnabled, streamListEmptyEnabled, initBranchEnabled)
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
	initBranchBefore, err := validateMailboxInitBranchTarget(mem, layout.initBranchSite)
	if err != nil {
		return false, err
	}
	invalidItemScanDesired := mailboxInvalidItemScanOriginal
	streamListEmptyDesired := mailboxStreamListEmptyOriginal
	initBranchDesired := mailboxInitBranchOriginal
	if enable {
		invalidItemScanDesired = mailboxInvalidItemScanPatched
		streamListEmptyDesired = mailboxStreamListEmptyPatched
		initBranchDesired = mailboxInitBranchPatched
	}
	// Install the list guard before redirecting Init into its list cleanup.
	patches := []struct {
		address         int64
		before, desired []byte
	}{
		{layout.streamListEmptySite, streamListEmptyBefore, streamListEmptyDesired},
		{layout.initBranchSite, initBranchBefore, initBranchDesired},
		{layout.invalidItemScanSite, invalidItemScanBefore, invalidItemScanDesired},
	}
	restores := make([]memoryRestore, 0, len(patches))
	for _, patch := range patches {
		if bytes.Equal(patch.before, patch.desired) {
			continue
		}
		// Include the current site: a failed write may have changed some bytes.
		restores = append([]memoryRestore{{address: patch.address, value: patch.before}}, restores...)
		if err := writeMemoryVerified(mem, patch.address, patch.desired); err != nil {
			return false, memoryPatchError("write mailbox guard", err, restoreMemoryVerified(mem, restores...))
		}
	}
	if len(restores) == 0 {
		return false, nil
	}
	actual, err := inspectMailboxGuardMemory(mem, layout)
	if err != nil || actual != enable {
		verificationErr := err
		if verificationErr == nil {
			verificationErr = fmt.Errorf("mailbox bad-node guard memory verification failed")
		}
		return false, memoryPatchError("verify mailbox bad-node guard", verificationErr, restoreMemoryVerified(mem, restores...))
	}
	return true, nil
}

func validateMailboxInitBranchTarget(mem io.ReaderAt, site int64) ([]byte, error) {
	prefix, err := readMemory(mem, site-int64(len(mailboxInitBranchPrefix)), len(mailboxInitBranchPrefix))
	if err != nil {
		return nil, err
	}
	current, err := readMemory(mem, site, len(mailboxInitBranchOriginal))
	if err != nil {
		return nil, err
	}
	reset, err := readMemory(mem, site+0xa9, len(mailboxInitResetPrefix))
	if err != nil {
		return nil, err
	}
	knownPatch := bytes.Equal(current, mailboxInitBranchOriginal) || bytes.Equal(current, mailboxInitBranchPatched)
	if !bytes.Equal(prefix, mailboxInitBranchPrefix) || !knownPatch || !bytes.Equal(reset, mailboxInitResetPrefix) {
		return nil, fmt.Errorf("%w near mailbox init branch: %x%x reset=%x", errMailboxGuardUnsupported, prefix, current, reset)
	}
	return current, nil
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
