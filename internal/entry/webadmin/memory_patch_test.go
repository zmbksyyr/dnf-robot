package webadmin

import (
	"errors"
	"strings"
	"testing"
)

type scheduledWriteFailure struct {
	memoryReadWriter
	calls map[int64]int
	fail  map[int64]map[int]error
}

func (m *scheduledWriteFailure) WriteAt(value []byte, address int64) (int, error) {
	if m.calls == nil {
		m.calls = make(map[int64]int)
	}
	m.calls[address]++
	if failures := m.fail[address]; failures != nil {
		if err := failures[m.calls[address]]; err != nil {
			return 0, err
		}
	}
	return m.memoryReadWriter.WriteAt(value, address)
}

func TestPartyCompatReportsRollbackFailure(t *testing.T) {
	layout := testPartyCompatLayout()
	mem := &scheduledWriteFailure{
		memoryReadWriter: newPartyCompatMemory(t, layout),
		fail: map[int64]map[int]error{
			layout.site:            {1: errors.New("branch write failure")},
			layout.rewardTimerSite: {2: errors.New("reward rollback failure")},
		},
	}

	_, err := setPartyCompatMemory(mem, layout, 17000000, 18000000, true)
	if err == nil || !strings.Contains(err.Error(), "branch write failure") || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "reward rollback failure") {
		t.Fatalf("patch error = %v", err)
	}
	cave, readErr := readMemory(mem, layout.cave, len(partyCompatZeroCave))
	if readErr != nil || !allZero(cave) {
		t.Fatalf("code cave was not rolled back: %x err=%v", cave, readErr)
	}
}

func TestMailboxGuardReportsRollbackFailure(t *testing.T) {
	file, layout := newMailboxGuardMemory(t)
	mem := &scheduledWriteFailure{
		memoryReadWriter: file,
		fail: map[int64]map[int]error{
			layout.invalidItemScanSite: {1: errors.New("invalid-item write failure")},
			layout.streamListEmptySite: {2: errors.New("stream-list rollback failure")},
		},
	}

	_, err := setMailboxGuardMemory(mem, layout, true)
	if err == nil || !strings.Contains(err.Error(), "invalid-item write failure") || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "stream-list rollback failure") {
		t.Fatalf("patch error = %v", err)
	}
}
