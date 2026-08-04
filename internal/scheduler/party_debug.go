package scheduler

import (
	"fmt"

	"robot/internal/shared"
)

type partyDebugRuntime interface {
	PartyDebugStart() shared.PartyDebugStatus
	PartyDebugStop() shared.PartyDebugStatus
	PartyDebugStatus() shared.PartyDebugStatus
}

func (m *RobotManager) PartyDebugStart() (shared.PartyDebugStatus, error) {
	runtime, ok := m.doll.(partyDebugRuntime)
	if !ok {
		return shared.PartyDebugStatus{}, fmt.Errorf("party debug is unavailable")
	}
	return runtime.PartyDebugStart(), nil
}

func (m *RobotManager) PartyDebugStop() (shared.PartyDebugStatus, error) {
	runtime, ok := m.doll.(partyDebugRuntime)
	if !ok {
		return shared.PartyDebugStatus{}, fmt.Errorf("party debug is unavailable")
	}
	return runtime.PartyDebugStop(), nil
}

func (m *RobotManager) PartyDebugStatus() (shared.PartyDebugStatus, error) {
	runtime, ok := m.doll.(partyDebugRuntime)
	if !ok {
		return shared.PartyDebugStatus{}, fmt.Errorf("party debug is unavailable")
	}
	return runtime.PartyDebugStatus(), nil
}
